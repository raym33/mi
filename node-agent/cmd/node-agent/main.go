package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/ollama"
	"github.com/raym33/mi/internal/protocol"
	"github.com/raym33/mi/internal/system"
	"github.com/raym33/mi/internal/wsutil"
)

type agent struct {
	cfg    config.NodeAgent
	ollama *ollama.Client
	active atomic.Int64
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

	a := &agent{cfg: cfg, ollama: ollama.New(cfg.OllamaURL)}
	for {
		if err := a.run(context.Background()); err != nil {
			log.Printf("agent disconnected: %v", err)
		}
		time.Sleep(2 * time.Second)
	}
}

func (a *agent) run(ctx context.Context) error {
	conn, _, err := websocket.Dial(ctx, a.cfg.CoordinatorURL, nil)
	if err != nil {
		return err
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	hostname, _ := os.Hostname()
	register := protocol.Envelope{
		Type: "register",
		Register: &protocol.Register{
			NodeID:        a.cfg.NodeID,
			ProviderID:    a.cfg.ProviderID,
			ProviderToken: a.cfg.ProviderToken,
			PublicName:    a.cfg.PublicName,
			City:          a.cfg.City,
			Hostname:      hostname,
			Arch:          runtime.GOARCH,
			OS:            runtime.GOOS,
			Models:        a.cfg.Models,
			MaxConcurrent: a.cfg.MaxConcurrent,
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

func (a *agent) heartbeatLoop(ctx context.Context, conn *safeConn) error {
	ticker := time.NewTicker(a.cfg.HeartbeatInterval.Duration)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			msg := protocol.Envelope{
				Type: "heartbeat",
				Heartbeat: &protocol.Heartbeat{
					NodeID:         a.cfg.NodeID,
					Models:         a.cfg.Models,
					ActiveRequests: int(a.active.Load()),
					QueueDepth:     0,
					MemoryFreeMB:   system.FreeMemoryMB(),
					LoadAverage:    loadAverage(),
					ObservedAt:     time.Now(),
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

	done, err := a.ollama.Chat(ctx, req, func(content string) error {
		return conn.writeJSON(ctx, protocol.Envelope{
			Type:      "chunk",
			RequestID: requestID,
			Chunk:     &protocol.InferChunk{Content: content},
		})
	})
	if err != nil {
		_ = conn.writeJSON(ctx, protocol.Envelope{
			Type:      "error",
			RequestID: requestID,
			Error:     &protocol.InferError{Message: err.Error(), Retryable: retryable(err)},
		})
		return
	}
	_ = conn.writeJSON(ctx, protocol.Envelope{Type: "done", RequestID: requestID, Done: &done})
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
