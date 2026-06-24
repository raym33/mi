package main

import (
	"context"
	"encoding/csv"
	"encoding/json"
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
	"github.com/raym33/mi/internal/openai"
	"github.com/raym33/mi/internal/protocol"
	"github.com/raym33/mi/internal/scheduler"
	"github.com/raym33/mi/internal/settlement"
	"github.com/raym33/mi/internal/wsutil"
)

// newE2EServer builds a coordinator with city local mode (no consumer auth) and
// an enabled settlement ledger, wired to a fresh mux/test server.
func newE2EServer(t *testing.T, adminToken string) (*server, *httptest.Server) {
	t.Helper()
	market, err := city.New(config.CityConfig{}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	settlements, err := settlement.New(config.SettlementConfig{
		Enabled:                      true,
		PricePerThousandTokensMicros: 1000,
		ProviderRewardShareBPS:       7000,
	})
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
		adminToken:   adminToken,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/node", s.nodeWebSocket)
	mux.HandleFunc("POST /v1/chat/completions", s.requireConsumerQuota(s.chatCompletions))
	mux.HandleFunc("GET /admin/metrics", s.requireAdmin(s.adminMetrics))
	mux.HandleFunc("GET /admin/payouts.csv", s.requireAdmin(s.adminPayoutsCSV))
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return s, ts
}

// dialE2ENode connects a node over a real WebSocket, registers it, and answers
// each infer request with a streamed chunk and a done frame, like the node-agent.
func dialE2ENode(t *testing.T, ctx context.Context, ts *httptest.Server, nodeID, providerID, model, content string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/node"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial node websocket: %v", err)
	}
	if err := wsutil.WriteJSON(ctx, conn, protocol.Envelope{
		Version: protocol.Version,
		Type:    "register",
		Register: &protocol.Register{
			NodeID:        nodeID,
			ProviderID:    providerID,
			PrivacyMode:   "private",
			Models:        []string{model},
			MaxConcurrent: 1,
		},
	}); err != nil {
		t.Fatalf("send register: %v", err)
	}
	go func() {
		for {
			msg, err := wsutil.ReadJSON[protocol.Envelope](ctx, conn)
			if err != nil {
				return
			}
			if msg.Type != "infer" || msg.Infer == nil {
				continue
			}
			_ = wsutil.WriteJSON(ctx, conn, protocol.Envelope{
				Version:   protocol.Version,
				Type:      "chunk",
				RequestID: msg.RequestID,
				Chunk:     &protocol.InferChunk{Content: content},
			})
			_ = wsutil.WriteJSON(ctx, conn, protocol.Envelope{
				Version:   protocol.Version,
				Type:      "done",
				RequestID: msg.RequestID,
				Done:      &protocol.InferDone{FinishReason: "stop", PromptTokens: 5, OutputTokens: 4},
			})
		}
	}()
	return conn
}

func e2eChat(t *testing.T, ts *httptest.Server, model string) openai.ChatCompletionResponse {
	t.Helper()
	body := `{"model":"` + model + `","privacy_tier":"private","messages":[{"role":"user","content":"hi"}],"stream":false}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("chat request: %v", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("chat status = %d, want 200: %s", resp.StatusCode, raw)
	}
	var completion openai.ChatCompletionResponse
	if err := json.Unmarshal(raw, &completion); err != nil {
		t.Fatalf("decode completion: %v: %s", err, raw)
	}
	return completion
}

// TestEndToEndChatOverWebSocket exercises the full path through the real HTTP
// server and a real WebSocket node connection. Nothing here stubs the transport.
func TestEndToEndChatOverWebSocket(t *testing.T) {
	s, ts := newE2EServer(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := dialE2ENode(t, ctx, ts, "e2e-node", "e2e-provider", "e2e-model", "hello from e2e node")
	defer conn.Close(websocket.StatusNormalClosure, "")

	waitFor(t, 5*time.Second, func() bool { return hasModel(s, "e2e-model") })

	completion := e2eChat(t, ts, "e2e-model")
	if len(completion.Choices) == 0 || completion.Choices[0].Message.Content != "hello from e2e node" {
		t.Fatalf("unexpected completion content: %+v", completion.Choices)
	}

	waitFor(t, 5*time.Second, func() bool { return s.settlement.Snapshot(10).Events == 1 })
	snap := s.settlement.Snapshot(10)
	if len(snap.ProviderBalances) != 1 || snap.ProviderBalances[0].AccountID != "e2e-provider" {
		t.Fatalf("expected one provider balance for e2e-provider, got %+v", snap.ProviderBalances)
	}
	if snap.RecentEvents[0].NodeID != "e2e-node" {
		t.Fatalf("settlement event node = %q, want e2e-node", snap.RecentEvents[0].NodeID)
	}
}

// TestEndToEndOperatorEndpointsReflectTraffic proves the operator surface shows
// real traffic: after a live completion, the Prometheus metrics and payout CSV
// (both admin-gated) reflect the settlement that just happened.
func TestEndToEndOperatorEndpointsReflectTraffic(t *testing.T) {
	const adminToken = "admin-e2e"
	s, ts := newE2EServer(t, adminToken)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	conn := dialE2ENode(t, ctx, ts, "ops-node", "ops-provider", "ops-model", "served")
	defer conn.Close(websocket.StatusNormalClosure, "")

	waitFor(t, 5*time.Second, func() bool { return hasModel(s, "ops-model") })
	e2eChat(t, ts, "ops-model")
	waitFor(t, 5*time.Second, func() bool { return s.settlement.Snapshot(10).Events == 1 })

	// Admin endpoints require the bearer token.
	if code := adminGetStatus(t, ts, "/admin/metrics", ""); code != http.StatusUnauthorized {
		t.Fatalf("metrics without token = %d, want 401", code)
	}

	metrics := adminGetBody(t, ts, "/admin/metrics", adminToken)
	if !strings.Contains(metrics, "mi_settlement_events_total") {
		t.Fatalf("metrics missing settlement counter:\n%s", metrics)
	}
	if !strings.Contains(metrics, `mi_provider_reward_micros{provider_id="ops-provider"}`) {
		t.Fatalf("metrics missing provider reward series for ops-provider:\n%s", metrics)
	}

	csvBody := adminGetBody(t, ts, "/admin/payouts.csv", adminToken)
	records, err := csv.NewReader(strings.NewReader(csvBody)).ReadAll()
	if err != nil {
		t.Fatalf("parse payout csv: %v\n%s", err, csvBody)
	}
	if len(records) < 2 {
		t.Fatalf("payout csv has no data rows:\n%s", csvBody)
	}
	if records[0][0] != "provider_id" {
		t.Fatalf("payout header = %v", records[0])
	}
	if records[1][0] != "ops-provider" {
		t.Fatalf("payout row provider = %q, want ops-provider", records[1][0])
	}
}

func hasModel(s *server, model string) bool {
	for _, m := range s.registry.Models() {
		if m == model {
			return true
		}
	}
	return false
}

func adminGetStatus(t *testing.T, ts *httptest.Server, path, token string) int {
	t.Helper()
	resp := adminGet(t, ts, path, token)
	defer resp.Body.Close()
	return resp.StatusCode
}

func adminGetBody(t *testing.T, ts *httptest.Server, path, token string) string {
	t.Helper()
	resp := adminGet(t, ts, path, token)
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET %s = %d, want 200", path, resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	return string(body)
}

func adminGet(t *testing.T, ts *httptest.Server, path, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, ts.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return resp
}

func waitFor(t *testing.T, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", timeout)
}
