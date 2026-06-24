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
	"strings"
	"testing"
	"time"

	"github.com/raym33/mi/internal/challenge"
	"github.com/raym33/mi/internal/city"
	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/modelcatalog"
	"github.com/raym33/mi/internal/openai"
	"github.com/raym33/mi/internal/protocol"
	"github.com/raym33/mi/internal/scheduler"
	"github.com/raym33/mi/internal/settlement"
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

func TestAdminDashboardServesShellWithoutExposingData(t *testing.T) {
	s := &server{adminToken: "admin-test"}
	req := httptest.NewRequest(http.MethodGet, "/admin/dashboard", nil)
	rec := httptest.NewRecorder()

	s.adminDashboard(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want %d", rec.Code, http.StatusOK)
	}
	if got := rec.Header().Get("Content-Type"); !strings.Contains(got, "text/html") {
		t.Fatalf("content-type = %q, want html", got)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "mi admin dashboard") || !strings.Contains(body, "/admin/reputation") {
		t.Fatalf("dashboard shell missing expected content")
	}
	if strings.Contains(body, "admin-test") {
		t.Fatalf("dashboard shell should not expose configured admin token")
	}
}

func TestAdminDashboardRedirect(t *testing.T) {
	s := &server{}
	req := httptest.NewRequest(http.MethodGet, "/admin", nil)
	rec := httptest.NewRecorder()

	s.adminDashboardRedirect(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("redirect status = %d, want %d", rec.Code, http.StatusFound)
	}
	if got := rec.Header().Get("Location"); got != "/admin/dashboard" {
		t.Fatalf("location = %q, want /admin/dashboard", got)
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

func TestChatRoutesCapabilityHints(t *testing.T) {
	market, err := city.New(config.CityConfig{}, []string{"sk-test"})
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	settlements, err := settlement.New(config.SettlementConfig{})
	if err != nil {
		t.Fatalf("new settlement: %v", err)
	}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{
		NodeID:       "mac-node",
		ProviderID:   "provider-mac",
		Backend:      "mlx",
		DeviceKind:   "mac",
		Accelerators: []string{"metal"},
		Models:       []string{"llama3.1:8b"},
	}, challengeNodeConn{content: "metal"})
	registry.Register(protocol.Register{
		NodeID:       "cuda-node",
		ProviderID:   "provider-cuda",
		Backend:      "vllm",
		DeviceKind:   "server",
		Accelerators: []string{"cuda"},
		Models:       []string{"llama3.1:8b"},
	}, challengeNodeConn{content: "cuda"})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "mac-node", Models: []string{"llama3.1:8b"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "cuda-node", Models: []string{"llama3.1:8b"}, LoadAverage: 10})
	s := &server{registry: registry, market: market, modelCatalog: modelcatalog.New(config.ModelConfig{}), settlement: settlements}
	handler := s.requireConsumerQuota(s.chatCompletions)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{
		"model": "llama3.1:8b",
		"mi_backend": "vllm",
		"mi_accelerators": ["cuda"],
		"messages": [{"role": "user", "content": "hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Mi-Node-Id") != "cuda-node" || rec.Header().Get("X-Mi-Backend") != "vllm" || rec.Header().Get("X-Mi-Accelerators") != "cuda" {
		t.Fatalf("headers node=%q backend=%q accelerators=%q, want cuda route", rec.Header().Get("X-Mi-Node-Id"), rec.Header().Get("X-Mi-Backend"), rec.Header().Get("X-Mi-Accelerators"))
	}
	var response openai.ChatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v: %s", err, rec.Body.String())
	}
	if response.Choices[0].Message.Content != "cuda" {
		t.Fatalf("content = %q, want cuda", response.Choices[0].Message.Content)
	}
}

func TestChatUsesCoordinatorMeasuredUsage(t *testing.T) {
	market, err := city.New(config.CityConfig{
		Enabled: true,
		Consumers: []config.ConsumerAccount{{
			ID:              "studio",
			TotalTokenLimit: 1000,
			APIKeys:         []string{"sk-test"},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	settlements, err := settlement.New(config.SettlementConfig{Enabled: true, PricePerThousandTokensMicros: 1000, ProviderRewardShareBPS: 7000})
	if err != nil {
		t.Fatalf("new settlement: %v", err)
	}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"llama3.1:8b"}, PrivacyMode: "private"}, maliciousUsageNodeConn{
		content: "ok",
		done:    protocol.InferDone{FinishReason: "stop", PromptTokens: 500000, OutputTokens: 500000},
	})
	s := &server{registry: registry, market: market, modelCatalog: modelcatalog.New(config.ModelConfig{}), settlement: settlements}
	handler := s.requireConsumerQuota(s.chatCompletions)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{
		"model": "llama3.1:8b",
		"max_tokens": 8,
		"messages": [{"role": "user", "content": "hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var response openai.ChatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v: %s", err, rec.Body.String())
	}
	if response.Usage.PromptTokens != 3 || response.Usage.CompletionTokens != 1 || response.Usage.TotalTokens != 4 {
		t.Fatalf("usage = %+v, want coordinator-estimated 3/1/4", response.Usage)
	}
	consumerStatus := market.ConsumerStatus("studio")
	if consumerStatus.Usage.TotalTokens != 4 {
		t.Fatalf("market usage = %+v, want total 4 not node-reported inflation", consumerStatus.Usage)
	}
	settlementSnapshot := settlements.Snapshot(10)
	if settlementSnapshot.Events != 1 || len(settlementSnapshot.RecentEvents) != 1 || settlementSnapshot.RecentEvents[0].TotalTokens != 4 {
		t.Fatalf("settlement = %+v, want one event with total tokens 4", settlementSnapshot)
	}
}

func TestChatUsesRuneMeasuredUsage(t *testing.T) {
	market, err := city.New(config.CityConfig{
		Enabled: true,
		Consumers: []config.ConsumerAccount{{
			ID:      "studio",
			APIKeys: []string{"sk-test"},
		}},
	}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	settlements, err := settlement.New(config.SettlementConfig{})
	if err != nil {
		t.Fatalf("new settlement: %v", err)
	}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"llama3.1:8b"}, PrivacyMode: "private"}, maliciousUsageNodeConn{
		content: "你好🙂",
		done:    protocol.InferDone{FinishReason: "stop", PromptTokens: 500000, OutputTokens: 500000},
	})
	s := &server{registry: registry, market: market, modelCatalog: modelcatalog.New(config.ModelConfig{}), settlement: settlements}
	handler := s.requireConsumerQuota(s.chatCompletions)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{
		"model": "llama3.1:8b",
		"messages": [{"role": "user", "content": "hola"}]
	}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var response openai.ChatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v: %s", err, rec.Body.String())
	}
	if response.Usage.PromptTokens != 2 || response.Usage.CompletionTokens != 1 || response.Usage.TotalTokens != 3 {
		t.Fatalf("usage = %+v, want rune-estimated 2/1/3", response.Usage)
	}
}

func TestChatAppliesDefaultMaxTokensWithoutQuota(t *testing.T) {
	market, err := city.New(config.CityConfig{}, []string{"sk-test"})
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	settlements, err := settlement.New(config.SettlementConfig{})
	if err != nil {
		t.Fatalf("new settlement: %v", err)
	}
	conn := &captureRequestConn{content: "ok"}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"llama3.1:8b"}, PrivacyMode: "private"}, conn)
	s := &server{registry: registry, market: market, modelCatalog: modelcatalog.New(config.ModelConfig{}), settlement: settlements}
	handler := s.requireConsumerQuota(s.chatCompletions)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{
		"model": "llama3.1:8b",
		"messages": [{"role": "user", "content": "hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if conn.req.MaxTokens == nil || *conn.req.MaxTokens != defaultReservedOutputTokens {
		t.Fatalf("max_tokens sent to node = %v, want default cap %d", conn.req.MaxTokens, defaultReservedOutputTokens)
	}
}

func TestChatRoutesAroundLowReputationProvider(t *testing.T) {
	market, err := city.New(config.CityConfig{
		Enabled: true,
		Consumers: []config.ConsumerAccount{{
			ID:      "studio",
			APIKeys: []string{"sk-test"},
		}},
		Providers: []config.ProviderAccount{
			{ID: "provider-shaky"},
			{ID: "provider-trusted"},
		},
	}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	settlements, err := settlement.New(config.SettlementConfig{})
	if err != nil {
		t.Fatalf("new settlement: %v", err)
	}
	challenges, err := challenge.New(config.ChallengeConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new challenges: %v", err)
	}
	if _, err := challenges.Record(challenge.RecordInput{ProviderID: "provider-shaky", Challenge: "synthetic-inference:llama3.1:8b", Passed: false, Score: 0}); err != nil {
		t.Fatalf("record challenge: %v", err)
	}
	shaky := &scriptedProviderConn{content: "shaky"}
	trusted := &scriptedProviderConn{content: "trusted"}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{NodeID: "node-shaky", ProviderID: "provider-shaky", Models: []string{"llama3.1:8b"}}, shaky)
	registry.Register(protocol.Register{NodeID: "node-trusted", ProviderID: "provider-trusted", Models: []string{"llama3.1:8b"}}, trusted)
	s := &server{registry: registry, market: market, modelCatalog: modelcatalog.New(config.ModelConfig{}), settlement: settlements, challenges: challenges}
	s.refreshSchedulerReputation()
	handler := s.requireConsumerQuota(s.chatCompletions)
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString(`{
		"model": "llama3.1:8b",
		"messages": [{"role": "user", "content": "hello"}]
	}`))
	req.Header.Set("Authorization", "Bearer sk-test")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	var response openai.ChatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v: %s", err, rec.Body.String())
	}
	if got := response.Choices[0].Message.Content; got != "trusted" {
		t.Fatalf("content = %q, want trusted provider selected by reputation", got)
	}
	if shaky.calls != 0 || trusted.calls != 1 {
		t.Fatalf("calls shaky=%d trusted=%d, want only trusted", shaky.calls, trusted.calls)
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

func TestSyntheticChallengeUsesOpaqueRequestID(t *testing.T) {
	challenges, err := challenge.New(config.ChallengeConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new challenges: %v", err)
	}
	conn := &recordRequestIDConn{content: "mi-ok"}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"llama3.1:8b"}, PrivacyMode: "public"}, conn)
	s := &server{registry: registry, challenges: challenges}

	if _, err := s.runSyntheticChallenge(context.Background(), config.ChallengeConfig{Model: "llama3.1:8b", ExpectedContains: "mi-ok"}); err != nil {
		t.Fatalf("run challenge: %v", err)
	}
	if strings.HasPrefix(conn.requestID, "challenge-") {
		t.Fatalf("challenge request id = %q, should be opaque", conn.requestID)
	}
	if !strings.HasPrefix(conn.requestID, "chatcmpl-") {
		t.Fatalf("challenge request id = %q, want normal chat-shaped id", conn.requestID)
	}
}

func TestSyntheticChallengePromptDoesNotExposeBenchmark(t *testing.T) {
	challenges, err := challenge.New(config.ChallengeConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new challenges: %v", err)
	}
	conn := &recordRequestIDConn{content: "mi-ok"}
	registry := scheduler.NewRegistry()
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"llama3.1:8b"}, PrivacyMode: "public"}, conn)
	s := &server{registry: registry, challenges: challenges}

	if _, err := s.runSyntheticChallenge(context.Background(), config.ChallengeConfig{Model: "llama3.1:8b", ExpectedContains: "mi-ok"}); err != nil {
		t.Fatalf("run challenge: %v", err)
	}
	joined := strings.ToLower(conn.req.Messages[0].Content + "\n" + conn.req.Messages[1].Content)
	for _, marker := range []string{"synthetic", "benchmark", "challenge"} {
		if strings.Contains(joined, marker) {
			t.Fatalf("challenge prompt leaked marker %q in %q", marker, joined)
		}
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

func TestAdminIntegrityBuildsAnchorManifest(t *testing.T) {
	settlements, err := settlement.New(config.SettlementConfig{Enabled: true, PricePerThousandTokensMicros: 1000, ProviderRewardShareBPS: 7000})
	if err != nil {
		t.Fatalf("new settlement: %v", err)
	}
	challenges, err := challenge.New(config.ChallengeConfig{Enabled: true})
	if err != nil {
		t.Fatalf("new challenges: %v", err)
	}
	if _, err := settlements.Record(settlement.RecordInput{
		RequestID:  "req-1",
		ConsumerID: "consumer-a",
		ProviderID: "provider-a",
		Model:      "llama3.1:8b",
		Done:       protocol.InferDone{PromptTokens: 3, OutputTokens: 5},
		Latency:    120 * time.Millisecond,
	}); err != nil {
		t.Fatalf("record settlement: %v", err)
	}
	if _, err := challenges.Record(challenge.RecordInput{ProviderID: "provider-a", Challenge: "latency", Passed: true, Score: 90}); err != nil {
		t.Fatalf("record challenge: %v", err)
	}
	s := &server{settlement: settlements, challenges: challenges}

	report := doJSON[integrityReport](t, http.HandlerFunc(s.adminIntegrity), http.MethodGet, "/admin/integrity", ``)
	if !report.Valid || !report.Settlement.Valid || !report.Challenges.Valid {
		t.Fatalf("report = %+v, want valid chains", report)
	}
	if report.Anchor.SettlementEvents != 1 || report.Anchor.ChallengeEvents != 1 || report.Anchor.AnchorHash == "" {
		t.Fatalf("anchor = %+v, want event counts and hash", report.Anchor)
	}
	if got := hashIntegrityAnchor(report.Anchor); got != report.Anchor.AnchorHash {
		t.Fatalf("anchor hash = %q, recomputed = %q", report.Anchor.AnchorHash, got)
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

type scriptedProviderConn struct {
	content string
	calls   int
}

func (c *scriptedProviderConn) SendInference(_ context.Context, _ string, _ protocol.InferRequest, sink scheduler.StreamSink) (protocol.InferDone, error) {
	c.calls++
	if c.content != "" {
		if err := sink.Chunk(c.content); err != nil {
			return protocol.InferDone{}, err
		}
	}
	return protocol.InferDone{FinishReason: "stop", PromptTokens: 1, OutputTokens: 1}, nil
}

func (c *scriptedProviderConn) Close() error {
	return nil
}

type maliciousUsageNodeConn struct {
	content string
	done    protocol.InferDone
}

func (c maliciousUsageNodeConn) SendInference(_ context.Context, _ string, _ protocol.InferRequest, sink scheduler.StreamSink) (protocol.InferDone, error) {
	if c.content != "" {
		if err := sink.Chunk(c.content); err != nil {
			return protocol.InferDone{}, err
		}
	}
	return c.done, nil
}

func (c maliciousUsageNodeConn) Close() error {
	return nil
}

type captureRequestConn struct {
	content string
	req     protocol.InferRequest
}

func (c *captureRequestConn) SendInference(_ context.Context, _ string, req protocol.InferRequest, sink scheduler.StreamSink) (protocol.InferDone, error) {
	c.req = req
	if c.content != "" {
		if err := sink.Chunk(c.content); err != nil {
			return protocol.InferDone{}, err
		}
	}
	return protocol.InferDone{FinishReason: "stop", PromptTokens: 1, OutputTokens: 1}, nil
}

func (c *captureRequestConn) Close() error {
	return nil
}

type recordRequestIDConn struct {
	requestID string
	content   string
	req       protocol.InferRequest
}

func (c *recordRequestIDConn) SendInference(_ context.Context, requestID string, req protocol.InferRequest, sink scheduler.StreamSink) (protocol.InferDone, error) {
	c.requestID = requestID
	c.req = req
	if c.content != "" {
		if err := sink.Chunk(c.content); err != nil {
			return protocol.InferDone{}, err
		}
	}
	return protocol.InferDone{FinishReason: "stop", PromptTokens: 1, OutputTokens: 1}, nil
}

func (c *recordRequestIDConn) Close() error {
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
