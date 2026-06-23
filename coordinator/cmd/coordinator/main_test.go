package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/raym33/mi/internal/challenge"
	"github.com/raym33/mi/internal/city"
	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/modelcatalog"
	"github.com/raym33/mi/internal/openai"
	"github.com/raym33/mi/internal/protocol"
	"github.com/raym33/mi/internal/scheduler"
)

func TestAdminEnrollmentRoutes(t *testing.T) {
	market, err := city.New(config.CityConfig{
		Enabled:        true,
		UsageStorePath: filepath.Join(t.TempDir(), "state.json"),
	}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	s := &server{registry: scheduler.NewRegistry(), market: market, modelCatalog: modelcatalog.New(config.ModelConfig{}), adminToken: "admin-test"}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /admin/consumers", s.requireAdmin(s.adminCreateConsumer))
	mux.HandleFunc("POST /admin/consumers/{id}/rotate-key", s.requireAdmin(s.adminRotateConsumerKey))
	mux.HandleFunc("DELETE /admin/consumers/{id}", s.requireAdmin(s.adminDisableConsumer))
	mux.HandleFunc("POST /admin/providers", s.requireAdmin(s.adminCreateProvider))
	mux.HandleFunc("POST /admin/providers/{id}/rotate-token", s.requireAdmin(s.adminRotateProviderToken))
	mux.HandleFunc("DELETE /admin/providers/{id}", s.requireAdmin(s.adminDisableProvider))

	createdConsumer := doJSON[city.CreatedConsumer](t, mux, http.MethodPost, "/admin/consumers", `{"id":"studio"}`)
	if createdConsumer.APIKey == "" {
		t.Fatal("created consumer should include one-time api key")
	}
	rotatedConsumer := doJSON[city.CreatedConsumer](t, mux, http.MethodPost, "/admin/consumers/studio/rotate-key", `{}`)
	if rotatedConsumer.APIKey == "" || rotatedConsumer.APIKey == createdConsumer.APIKey {
		t.Fatalf("rotated key = %q, old = %q", rotatedConsumer.APIKey, createdConsumer.APIKey)
	}
	disabledConsumer := doJSON[map[string]city.Consumer](t, mux, http.MethodDelete, "/admin/consumers/studio", `{}`)
	if !disabledConsumer["consumer"].Disabled {
		t.Fatal("consumer should be disabled")
	}

	createdProvider := doJSON[city.CreatedProvider](t, mux, http.MethodPost, "/admin/providers", `{"id":"provider-a"}`)
	if createdProvider.ProviderToken == "" {
		t.Fatal("created provider should include one-time provider token")
	}
	rotatedProvider := doJSON[city.CreatedProvider](t, mux, http.MethodPost, "/admin/providers/provider-a/rotate-token", `{}`)
	if rotatedProvider.ProviderToken == "" || rotatedProvider.ProviderToken == createdProvider.ProviderToken {
		t.Fatalf("rotated token = %q, old = %q", rotatedProvider.ProviderToken, createdProvider.ProviderToken)
	}
	disabledProvider := doJSON[struct {
		Provider          city.Provider `json:"provider"`
		DisconnectedNodes int           `json:"disconnected_nodes"`
	}](t, mux, http.MethodDelete, "/admin/providers/provider-a", `{}`)
	if !disabledProvider.Provider.Disabled {
		t.Fatal("provider should be disabled")
	}
}

func TestAdminRequiresTokenUnlessDevOpen(t *testing.T) {
	market, err := city.New(config.CityConfig{}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	s := &server{registry: scheduler.NewRegistry(), market: market, modelCatalog: modelcatalog.New(config.ModelConfig{})}
	req := httptest.NewRequest(http.MethodGet, "/admin/nodes", nil)
	rec := httptest.NewRecorder()
	s.requireAdmin(s.adminNodes)(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("admin without token status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	s.devAdminOpen = true
	rec = httptest.NewRecorder()
	s.requireAdmin(s.adminNodes)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("dev-open admin status = %d, want %d: %s", rec.Code, http.StatusOK, rec.Body.String())
	}
}

func TestNodeWebSocketRequiresClientCertificate(t *testing.T) {
	market, err := city.New(config.CityConfig{}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	s := &server{registry: scheduler.NewRegistry(), market: market, modelCatalog: modelcatalog.New(config.ModelConfig{}), requireNodeClientCert: true}

	req := httptest.NewRequest(http.MethodGet, "/ws/node", nil)
	rec := httptest.NewRecorder()
	s.nodeWebSocket(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing client cert status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}

	req = httptest.NewRequest(http.MethodGet, "/ws/node", nil)
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{}}}
	rec = httptest.NewRecorder()
	s.nodeWebSocket(rec, req)
	if rec.Code == http.StatusUnauthorized {
		t.Fatal("request with client cert should pass mTLS gate")
	}
}

func TestModelAliasesAppearWhenTargetAvailable(t *testing.T) {
	market, err := city.New(config.CityConfig{}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{NodeID: "node-a", Models: []string{"llama3.1:8b"}}, noopNodeConn{})
	s := &server{
		registry: registry,
		market:   market,
		modelCatalog: modelcatalog.New(config.ModelConfig{Aliases: []config.ModelAlias{{
			ID:          "fast",
			Target:      "llama3.1:8b",
			DisplayName: "Fast local",
		}}}),
	}

	models := doJSON[openai.ModelList](t, http.HandlerFunc(s.models), http.MethodGet, "/v1/models", ``)
	ids := map[string]bool{}
	for _, model := range models.Data {
		ids[model.ID] = true
	}
	if !ids["fast"] || !ids["llama3.1:8b"] {
		t.Fatalf("models = %+v, want alias and concrete model", models.Data)
	}

	catalog := doJSON[modelcatalog.CatalogResponse](t, http.HandlerFunc(s.modelsCatalog), http.MethodGet, "/v1/models/catalog", ``)
	if len(catalog.Data) != 2 || catalog.Data[0].ID != "fast" || !catalog.Data[0].Available {
		t.Fatalf("catalog = %+v, want available fast alias first", catalog.Data)
	}
}

func TestChatRejectsInvalidPrivacyTier(t *testing.T) {
	market, err := city.New(config.CityConfig{}, []string{"sk-test"})
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	s := &server{registry: scheduler.NewRegistry(), market: market, modelCatalog: modelcatalog.New(config.ModelConfig{})}
	handler := s.requireConsumerQuota(s.chatCompletions)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{
		"model": "llama3.1:8b",
		"privacy_tier": "secret",
		"messages": [{"role": "user", "content": "hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d: %s", rec.Code, http.StatusBadRequest, rec.Body.String())
	}
}

func TestRunSyntheticChallengeRecordsProviderEvidence(t *testing.T) {
	market, err := city.New(config.CityConfig{}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	challenges, err := challenge.New(config.ChallengeConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new challenges: %v", err)
	}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{
		NodeID:      "node-a",
		ProviderID:  "provider-a",
		Models:      []string{"llama3.1:8b"},
		PrivacyMode: "private",
	}, challengeNodeConn{content: "mi-ok"})
	s := &server{
		registry:     registry,
		market:       market,
		modelCatalog: modelcatalog.New(config.ModelConfig{}),
		challenges:   challenges,
	}

	event, err := s.runSyntheticChallenge(context.Background(), config.ChallengeConfig{
		Model:            "llama3.1:8b",
		ExpectedContains: "mi-ok",
		MaxTokens:        4,
	})
	if err != nil {
		t.Fatalf("run challenge: %v", err)
	}
	if !event.Passed || event.ProviderID != "provider-a" || event.NodeID != "node-a" || event.Score != 100 {
		t.Fatalf("event = %+v, want passing provider evidence", event)
	}
	if snapshot := challenges.Snapshot(10); snapshot.Events != 1 || len(snapshot.Summaries) != 1 || snapshot.Summaries[0].PassRateBPS != 10000 {
		t.Fatalf("snapshot = %+v, want one perfect summary", snapshot)
	}
}

func TestSyntheticChallengeRotatesProviders(t *testing.T) {
	challenges, err := challenge.New(config.ChallengeConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new challenges: %v", err)
	}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"llama3.1:8b"}, PrivacyMode: "public"}, challengeNodeConn{content: "mi-ok"})
	registry.Register(protocol.Register{NodeID: "node-b", ProviderID: "provider-b", Models: []string{"llama3.1:8b"}, PrivacyMode: "public"}, challengeNodeConn{content: "mi-ok"})
	s := &server{registry: registry, challenges: challenges}

	first, err := s.runSyntheticChallenge(context.Background(), config.ChallengeConfig{Model: "llama3.1:8b", ExpectedContains: "mi-ok"})
	if err != nil {
		t.Fatalf("first challenge: %v", err)
	}
	second, err := s.runSyntheticChallenge(context.Background(), config.ChallengeConfig{Model: "llama3.1:8b", ExpectedContains: "mi-ok"})
	if err != nil {
		t.Fatalf("second challenge: %v", err)
	}
	if first.ProviderID == second.ProviderID {
		t.Fatalf("providers = %q then %q, want rotation", first.ProviderID, second.ProviderID)
	}
}

type noopNodeConn struct{}

func (noopNodeConn) SendInference(context.Context, string, protocol.InferRequest, scheduler.StreamSink) (protocol.InferDone, error) {
	return protocol.InferDone{}, nil
}

func (noopNodeConn) Close() error {
	return nil
}

type challengeNodeConn struct {
	content string
}

func (c challengeNodeConn) SendInference(_ context.Context, _ string, _ protocol.InferRequest, sink scheduler.StreamSink) (protocol.InferDone, error) {
	if c.content != "" {
		if err := sink.Chunk(c.content); err != nil {
			return protocol.InferDone{}, err
		}
	}
	return protocol.InferDone{FinishReason: "stop", PromptTokens: 6, OutputTokens: 2}, nil
}

func (c challengeNodeConn) Close() error {
	return nil
}

func doJSON[T any](t *testing.T, handler http.Handler, method, path, body string) T {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer admin-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code < 200 || rec.Code >= 300 {
		t.Fatalf("%s %s returned %d: %s", method, path, rec.Code, rec.Body.String())
	}
	var value T
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response: %v: %s", err, rec.Body.String())
	}
	return value
}
