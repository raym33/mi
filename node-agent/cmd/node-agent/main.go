package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"flag"
	"log"
	"math/rand"
	"net"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/raym33/mi/internal/backend"
	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/ollama"
	"github.com/raym33/mi/internal/privacy"
	"github.com/raym33/mi/internal/protocol"
	"github.com/raym33/mi/internal/system"
	"github.com/raym33/mi/internal/wsutil"
)

type agent struct {
	cfg     config.NodeAgent
	backend backend.Runtime
	active  atomic.Int64
}

func main() {
	configPath := flag.String("config", "configs/node-agent.yaml", "path to node-agent config")
	flag.Parse()

	cfg, err := config.LoadNodeAgent(*configPath)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.NodeID == "" {
		cfg.NodeID = defaultNodeID()
	}

	runtimeBackend, err := newInferenceBackend(cfg)
	if err != nil {
		log.Fatal(err)
	}

	a := &agent{cfg: cfg, backend: runtimeBackend}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	for ctx.Err() == nil {
		if err := a.run(ctx); err != nil {
			log.Printf("agent disconnected: %v", err)
		}
		// Back off with jitter so a coordinator restart does not trigger a
		// synchronized reconnect storm across the fleet.
		select {
		case <-ctx.Done():
		case <-time.After(reconnectDelay()):
		}
	}
	log.Printf("node agent stopped")
}

const (
	reconnectBaseDelay   = 1 * time.Second
	reconnectJitterRange = 2 * time.Second
)

func reconnectDelay() time.Duration {
	return reconnectBaseDelay + time.Duration(rand.Int63n(int64(reconnectJitterRange)))
}

func (a *agent) run(ctx context.Context) error {
	dialOptions, err := dialOptions(a.cfg)
	if err != nil {
		return err
	}
	privacyTiers, err := nodePrivacyTiers(a.cfg)
	if err != nil {
		return err
	}
	conn, _, err := websocket.Dial(ctx, a.cfg.CoordinatorURL, dialOptions)
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hostname, _ := os.Hostname()
	register := protocol.Envelope{
		Version: protocol.Version,
		Type:    "register",
		Register: &protocol.Register{
			ProtocolVersion: protocol.Version,
			NodeID:          a.cfg.NodeID,
			ProviderID:      a.cfg.ProviderID,
			ProviderToken:   a.cfg.ProviderToken,
			PublicName:      a.cfg.PublicName,
			City:            a.cfg.City,
			PrivacyMode:     a.cfg.PrivacyMode,
			PrivacyTiers:    privacyTiers,
			Hostname:        hostname,
			Arch:            runtime.GOARCH,
			OS:              runtime.GOOS,
			Backend:         a.backend.Name(),
			DeviceKind:      a.cfg.Hardware.Kind,
			DeviceVendor:    a.cfg.Hardware.Vendor,
			DeviceModel:     a.cfg.Hardware.Model,
			SoC:             a.cfg.Hardware.SoC,
			Accelerators:    a.cfg.Hardware.Accelerators,
			PowerMode:       a.cfg.Hardware.PowerMode,
			NetworkMode:     a.cfg.Hardware.NetworkMode,
			Models:          a.cfg.Models,
			MaxConcurrent:   a.cfg.MaxConcurrent,
		},
	}
	if err := wsutil.WriteJSON(ctx, conn, register); err != nil {
		return err
	}
	log.Printf("registered node %s with coordinator %s", a.cfg.NodeID, a.cfg.CoordinatorURL)

	errCh := make(chan error, 2)
	safe := &safeConn{conn: conn}
	go func() { errCh <- a.heartbeatLoop(ctx, safe) }()
	go func() { errCh <- a.readLoop(ctx, safe) }()
	return <-errCh
}

func nodePrivacyTiers(cfg config.NodeAgent) ([]string, error) {
	if len(cfg.PrivacyTiers) > 0 {
		return privacy.NormalizeTiers(cfg.PrivacyTiers)
	}
	return privacy.TiersForMode(cfg.PrivacyMode)
}

func (a *agent) heartbeatLoop(ctx context.Context, conn *safeConn) error {
	ticker := time.NewTicker(a.cfg.HeartbeatInterval.Duration)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			msg := protocol.Envelope{
				Version: protocol.Version,
				Type:    "heartbeat",
				Heartbeat: &protocol.Heartbeat{
					ProtocolVersion: protocol.Version,
					NodeID:          a.cfg.NodeID,
					Models:          a.cfg.Models,
					ActiveRequests:  int(a.active.Load()),
					QueueDepth:      0,
					MemoryFreeMB:    system.FreeMemoryMB(),
					LoadAverage:     loadAverage(),
					ObservedAt:      time.Now(),
				},
			}
			if err := conn.writeJSON(ctx, msg); err != nil {
				return err
			}
		}
	}
}

func (a *agent) readLoop(ctx context.Context, conn *safeConn) error {
	for {
		msg, err := wsutil.ReadJSON[protocol.Envelope](ctx, conn.conn)
		if err != nil {
			return err
		}
		if msg.Type != "infer" || msg.Infer == nil {
			continue
		}
		if int(a.active.Load()) >= a.cfg.MaxConcurrent {
			_ = conn.writeJSON(ctx, protocol.Envelope{
				Version:   protocol.Version,
				Type:      "error",
				RequestID: msg.RequestID,
				Error:     &protocol.InferError{Message: "node at capacity", Retryable: true},
			})
			continue
		}
		go a.handleInference(ctx, conn, msg.RequestID, *msg.Infer)
	}
}

func (a *agent) handleInference(ctx context.Context, conn *safeConn, requestID string, req protocol.InferRequest) {
	a.active.Add(1)
	defer a.active.Add(-1)

	done, err := a.backend.Chat(ctx, req, func(content string) error {
		return conn.writeJSON(ctx, protocol.Envelope{
			Version:   protocol.Version,
			Type:      "chunk",
			RequestID: requestID,
			Chunk:     &protocol.InferChunk{Content: content},
		})
	})
	if err != nil {
		_ = conn.writeJSON(ctx, protocol.Envelope{
			Version:   protocol.Version,
			Type:      "error",
			RequestID: requestID,
			Error:     &protocol.InferError{Message: err.Error(), Retryable: retryable(err)},
		})
		return
	}
	_ = conn.writeJSON(ctx, protocol.Envelope{Version: protocol.Version, Type: "done", RequestID: requestID, Done: &done})
}

func newInferenceBackend(cfg config.NodeAgent) (backend.Runtime, error) {
	switch strings.ToLower(cfg.Backend.Type) {
	case "", "ollama":
		return ollama.New(cfg.Backend.URL), nil
	default:
		return nil, errors.New("unsupported backend type: " + cfg.Backend.Type)
	}
}

type safeConn struct {
	conn *websocket.Conn
	mu   sync.Mutex
}

func (s *safeConn) writeJSON(ctx context.Context, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return wsutil.WriteJSON(ctx, s.conn, value)
}

func retryable(err error) bool {
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

func defaultNodeID() string {
	hostname, err := os.Hostname()
	if err != nil || hostname == "" {
		return "mi-node"
	}
	return hostname
}

func loadAverage() float64 {
	ifaces, err := net.Interfaces()
	if err != nil {
		return 0
	}
	up := 0
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp != 0 {
			up++
		}
	}
	return float64(runtime.NumGoroutine()+up) / 100
}

func dialOptions(cfg config.NodeAgent) (*websocket.DialOptions, error) {
	if cfg.TLS.CAFile == "" && cfg.TLS.CertFile == "" && cfg.TLS.KeyFile == "" && !cfg.TLS.InsecureSkipVerify {
		return nil, nil
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, InsecureSkipVerify: cfg.TLS.InsecureSkipVerify}
	if cfg.TLS.CAFile != "" {
		certPEM, err := os.ReadFile(cfg.TLS.CAFile)
		if err != nil {
			return nil, err
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(certPEM) {
			return nil, errors.New("failed to parse tls.ca_file")
		}
		tlsConfig.RootCAs = roots
	}
	if cfg.TLS.CertFile != "" || cfg.TLS.KeyFile != "" {
		if cfg.TLS.CertFile == "" || cfg.TLS.KeyFile == "" {
			return nil, errors.New("both tls.cert_file and tls.key_file are required")
		}
		cert, err := tls.LoadX509KeyPair(cfg.TLS.CertFile, cfg.TLS.KeyFile)
		if err != nil {
			return nil, err
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}
	return &websocket.DialOptions{
		HTTPClient: &http.Client{
			Transport: &http.Transport{TLSClientConfig: tlsConfig},
		},
	}, nil
}
