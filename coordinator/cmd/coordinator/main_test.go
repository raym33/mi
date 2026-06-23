package main

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/raym33/mi/internal/city"
	"github.com/raym33/mi/internal/config"
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
	s := &server{registry: scheduler.NewRegistry(), market: market, adminToken: "admin-test"}

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

func TestNodeWebSocketRequiresClientCertificate(t *testing.T) {
	market, err := city.New(config.CityConfig{}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	s := &server{registry: scheduler.NewRegistry(), market: market, requireNodeClientCert: true}

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
