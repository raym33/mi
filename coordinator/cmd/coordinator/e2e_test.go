package main

import (
	"context"
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

// TestEndToEndChatOverWebSocket exercises the full path through the real HTTP
// server and a real WebSocket node connection: a node dials /ws/node, registers,
// and streams inference output; a consumer calls /v1/chat/completions and the
// coordinator routes, streams, measures usage, and records a settlement event.
// Unlike the other handler tests, nothing here stubs the transport.
func TestEndToEndChatOverWebSocket(t *testing.T) {
	market, err := city.New(config.CityConfig{}, nil) // local mode: no auth required
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
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /ws/node", s.nodeWebSocket)
	mux.HandleFunc("POST /v1/chat/completions", s.requireConsumerQuota(s.chatCompletions))
	ts := httptest.NewServer(mux)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// A node dials in and behaves like the node-agent: register, then answer
	// each infer request with a streamed chunk and a done frame.
	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws/node"
	conn, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial node websocket: %v", err)
	}
	defer conn.Close(websocket.StatusNormalClosure, "")

	if err := wsutil.WriteJSON(ctx, conn, protocol.Envelope{
		Version: protocol.Version,
		Type:    "register",
		Register: &protocol.Register{
			NodeID:        "e2e-node",
			ProviderID:    "e2e-provider",
			PrivacyMode:   "private",
			Models:        []string{"e2e-model"},
			MaxConcurrent: 1,
		},
	}); err != nil {
		t.Fatalf("send register: %v", err)
	}

	nodeDone := make(chan struct{})
	go func() {
		defer close(nodeDone)
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
				Chunk:     &protocol.InferChunk{Content: "hello from e2e node"},
			})
			_ = wsutil.WriteJSON(ctx, conn, protocol.Envelope{
				Version:   protocol.Version,
				Type:      "done",
				RequestID: msg.RequestID,
				Done:      &protocol.InferDone{FinishReason: "stop", PromptTokens: 5, OutputTokens: 4},
			})
		}
	}()

	// Wait for the node to finish registering before sending traffic.
	waitFor(t, 5*time.Second, func() bool {
		for _, m := range s.registry.Models() {
			if m == "e2e-model" {
				return true
			}
		}
		return false
	})

	reqBody := `{"model":"e2e-model","privacy_tier":"private","messages":[{"role":"user","content":"hi"}],"stream":false}`
	resp, err := http.Post(ts.URL+"/v1/chat/completions", "application/json", strings.NewReader(reqBody))
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
	if len(completion.Choices) == 0 || completion.Choices[0].Message.Content != "hello from e2e node" {
		t.Fatalf("unexpected completion content: %s", raw)
	}

	// The coordinator should have measured usage and recorded exactly one
	// settlement event attributed to the connected provider.
	waitFor(t, 5*time.Second, func() bool {
		return settlements.Snapshot(10).Events == 1
	})
	snap := settlements.Snapshot(10)
	if len(snap.ProviderBalances) != 1 || snap.ProviderBalances[0].AccountID != "e2e-provider" {
		t.Fatalf("expected one provider balance for e2e-provider, got %+v", snap.ProviderBalances)
	}
	if snap.RecentEvents[0].NodeID != "e2e-node" {
		t.Fatalf("settlement event node = %q, want e2e-node", snap.RecentEvents[0].NodeID)
	}
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
