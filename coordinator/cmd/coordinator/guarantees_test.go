package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/raym33/mi/internal/challenge"
	"github.com/raym33/mi/internal/city"
	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/modelcatalog"
	"github.com/raym33/mi/internal/scheduler"
	"github.com/raym33/mi/internal/settlement"
)

// chatRequest posts a non-streaming completion with an optional privacy tier and
// API key, returning the status code and raw body.
func chatRequest(t *testing.T, ts *httptest.Server, model, privacyTier, apiKey string) (int, string) {
	t.Helper()
	body := fmt.Sprintf(`{"model":%q,"privacy_tier":%q,"messages":[{"role":"user","content":"hi"}],"stream":false}`, model, privacyTier)
	req, err := http.NewRequest(http.MethodPost, ts.URL+"/v1/chat/completions", strings.NewReader(body))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("chat request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(raw)
}

// TestEndToEndPrivacyRoutingRejectsPublicNodeForPrivate proves the privacy tier
// is enforced at routing: a public-only node must not receive private work
// (returns 503, no eligible node), but serves public work.
func TestEndToEndPrivacyRoutingRejectsPublicNodeForPrivate(t *testing.T) {
	s, ts := newE2EServer(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := dialE2ENode(t, ctx, ts, "pub-node", "pub-provider", "priv-model", "served", 4, "public")
	defer conn.Close(websocket.StatusNormalClosure, "")
	waitFor(t, 5*time.Second, func() bool { return hasModel(s, "priv-model") })

	if code, body := chatRequest(t, ts, "priv-model", "private", ""); code != http.StatusServiceUnavailable {
		t.Fatalf("private request to public-only node = %d, want 503: %s", code, body)
	}
	if code, body := chatRequest(t, ts, "priv-model", "public", ""); code != http.StatusOK {
		t.Fatalf("public request = %d, want 200: %s", code, body)
	} else if !strings.Contains(body, "served") {
		t.Fatalf("public completion missing content: %s", body)
	}
}

// TestEndToEndQuotaEnforcement proves per-consumer quotas are enforced: an
// unlimited consumer succeeds, a consumer over its token limit is rejected with
// 402 before any dispatch.
func TestEndToEndQuotaEnforcement(t *testing.T) {
	market, err := city.New(config.CityConfig{
		Enabled: true,
		Consumers: []config.ConsumerAccount{
			{ID: "open", DisplayName: "Open", APIKeys: []string{"sk-open"}, TotalTokenLimit: 0},
			{ID: "capped", DisplayName: "Capped", APIKeys: []string{"sk-capped"}, TotalTokenLimit: 1},
		},
	}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	settlements, err := settlement.New(config.SettlementConfig{Enabled: true, PricePerThousandTokensMicros: 1000, ProviderRewardShareBPS: 7000})
	if err != nil {
		t.Fatalf("new settlement: %v", err)
	}
	challenges, err := challenge.New(config.ChallengeConfig{})
	if err != nil {
		t.Fatalf("new challenges: %v", err)
	}
	s := &server{
		registry:     scheduler.NewRegistry(),
		market:       market,
		modelCatalog: modelcatalog.New(config.ModelConfig{}),
		settlement:   settlements,
		challenges:   challenges,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/node", s.nodeWebSocket)
	mux.HandleFunc("POST /v1/chat/completions", s.requireConsumerQuota(s.chatCompletions))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn := dialE2ENode(t, ctx, ts, "quota-node", "quota-provider", "quota-model", "ok", 4, "private")
	defer conn.Close(websocket.StatusNormalClosure, "")
	waitFor(t, 5*time.Second, func() bool { return hasModel(s, "quota-model") })

	// Missing key is unauthorized.
	if code, _ := chatRequest(t, ts, "quota-model", "private", ""); code != http.StatusUnauthorized {
		t.Fatalf("no api key = %d, want 401", code)
	}
	// Unlimited consumer succeeds.
	if code, body := chatRequest(t, ts, "quota-model", "private", "sk-open"); code != http.StatusOK {
		t.Fatalf("open consumer = %d, want 200: %s", code, body)
	}
	// Capped consumer is rejected before dispatch with a quota error.
	code, body := chatRequest(t, ts, "quota-model", "private", "sk-capped")
	if code != http.StatusPaymentRequired {
		t.Fatalf("capped consumer = %d, want 402: %s", code, body)
	}
	if !strings.Contains(body, "quota_exceeded") {
		t.Fatalf("capped consumer body missing quota_exceeded: %s", body)
	}
}
