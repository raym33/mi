package main

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/raym33/mi/internal/config"
)

func TestNewHTTPServerValidatesTLSConfig(t *testing.T) {
	handler := http.NewServeMux()

	if _, err := newHTTPServer(config.Coordinator{}, handler); err != nil {
		t.Fatalf("plain config should be valid: %v", err)
	}
	if _, err := newHTTPServer(config.Coordinator{TLS: config.ServerTLSConfig{CertFile: "cert.pem"}}, handler); err == nil {
		t.Fatal("cert without key should be rejected")
	}
	if _, err := newHTTPServer(config.Coordinator{TLS: config.ServerTLSConfig{KeyFile: "key.pem"}}, handler); err == nil {
		t.Fatal("key without cert should be rejected")
	}
	if _, err := newHTTPServer(config.Coordinator{TLS: config.ServerTLSConfig{NodeClientCAFile: "ca.pem"}}, handler); err == nil {
		t.Fatal("node client CA without server cert/key should be rejected")
	}
}

// TestGracefulShutdownWaitsForInFlightRequest proves that Shutdown drains an
// in-flight request instead of cutting it off: Shutdown must not return until
// the slow handler has completed.
func TestGracefulShutdownWaitsForInFlightRequest(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})

	handler := http.NewServeMux()
	handler.HandleFunc("/slow", func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})

	srv, err := newHTTPServer(config.Coordinator{}, handler)
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	go func() { _ = srv.Serve(listener) }()

	requestDone := make(chan struct{})
	go func() {
		resp, err := http.Get("http://" + listener.Addr().String() + "/slow")
		if err == nil {
			_ = resp.Body.Close()
		}
		close(requestDone)
	}()

	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("slow handler never started")
	}

	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- srv.Shutdown(context.Background()) }()

	// Shutdown must block while the request is in flight.
	select {
	case <-shutdownDone:
		t.Fatal("Shutdown returned before the in-flight request finished")
	case <-time.After(150 * time.Millisecond):
	}

	close(release)

	select {
	case err := <-shutdownDone:
		if err != nil {
			t.Fatalf("Shutdown returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown did not complete after request finished")
	}
	<-requestDone
}
