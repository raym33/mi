package scheduler

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/raym33/mi/internal/privacy"
	"github.com/raym33/mi/internal/protocol"
)

type fakeConn struct {
	closed bool
}

func (f *fakeConn) SendInference(context.Context, string, protocol.InferRequest, StreamSink) (protocol.InferDone, error) {
	return protocol.InferDone{}, nil
}

func (f *fakeConn) Close() error {
	f.closed = true
	return nil
}

func TestRemoveProviderDisconnectsOnlyThatProvider(t *testing.T) {
	registry := NewRegistry()
	a := &fakeConn{}
	b := &fakeConn{}
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"m"}}, a)
	registry.Register(protocol.Register{NodeID: "node-b", ProviderID: "provider-b", Models: []string{"m"}}, b)

	removed := registry.RemoveProvider("provider-a")
	if removed != 1 {
		t.Fatalf("removed = %d, want 1", removed)
	}
	if !a.closed {
		t.Fatal("provider-a node should be closed")
	}
	if b.closed {
		t.Fatal("provider-b node should stay connected")
	}

	nodes := registry.Snapshot()
	if len(nodes) != 1 || nodes[0].ProviderID != "provider-b" {
		t.Fatalf("remaining nodes = %+v, want provider-b only", nodes)
	}
}

func TestDispatchRetriesBeforeFirstChunk(t *testing.T) {
	registry := NewRegistry()
	first := &scriptedConn{err: errors.New("cold start failed")}
	second := &scriptedConn{done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"m"}}, first)
	registry.Register(protocol.Register{NodeID: "node-b", ProviderID: "provider-b", Models: []string{"m"}}, second)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-a", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-b", Models: []string{"m"}, LoadAverage: 10})

	result, err := registry.Dispatch(context.Background(), "req", protocol.InferRequest{Model: "m"}, &collectTestSink{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.Attempts != 2 || result.NodeID != "node-b" || result.ProviderID != "provider-b" {
		t.Fatalf("result = %+v, want retry to node-b after 2 attempts", result)
	}
	if first.calls != 1 || second.calls != 1 {
		t.Fatalf("calls first=%d second=%d, want 1 each", first.calls, second.calls)
	}
	nodes := registry.Snapshot()
	for _, node := range nodes {
		if node.ID == "node-a" && (!node.InCooldown || node.ErrorStreak != 1 || node.LastError == "") {
			t.Fatalf("node-a cooldown state = %+v, want cooldown after pre-token failure", node)
		}
	}
}

func TestDispatchToProviderTargetsOnlyThatProvider(t *testing.T) {
	registry := NewRegistry()
	first := &scriptedConn{done: protocol.InferDone{FinishReason: "stop"}}
	second := &scriptedConn{chunk: "target", done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"m"}}, first)
	registry.Register(protocol.Register{NodeID: "node-b", ProviderID: "provider-b", Models: []string{"m"}}, second)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-a", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-b", Models: []string{"m"}, LoadAverage: 10})

	sink := &collectTestSink{}
	result, err := registry.DispatchToProvider(context.Background(), "req", "provider-b", protocol.InferRequest{Model: "m"}, sink)
	if err != nil {
		t.Fatalf("dispatch to provider: %v", err)
	}
	if result.NodeID != "node-b" || result.ProviderID != "provider-b" || first.calls != 0 || second.calls != 1 {
		t.Fatalf("result = %+v first=%d second=%d, want provider-b only", result, first.calls, second.calls)
	}
	if sink.content != "target" {
		t.Fatalf("sink content = %q, want target", sink.content)
	}
}

func TestDispatchDoesNotRetryAfterFirstChunk(t *testing.T) {
	registry := NewRegistry()
	first := &scriptedConn{chunk: "partial", err: errors.New("decode failed")}
	second := &scriptedConn{done: protocol.InferDone{FinishReason: "stop"}}
	sink := &collectTestSink{}
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"m"}}, first)
	registry.Register(protocol.Register{NodeID: "node-b", ProviderID: "provider-b", Models: []string{"m"}}, second)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-a", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-b", Models: []string{"m"}, LoadAverage: 10})

	result, err := registry.Dispatch(context.Background(), "req", protocol.InferRequest{Model: "m"}, sink)
	if err == nil {
		t.Fatal("dispatch should return node-a error")
	}
	if result.Attempts != 1 || result.NodeID != "node-a" {
		t.Fatalf("result = %+v, want single failed attempt on node-a", result)
	}
	if second.calls != 0 {
		t.Fatalf("second calls = %d, want no retry after first chunk", second.calls)
	}
	if sink.content != "partial" {
		t.Fatalf("sink content = %q, want partial", sink.content)
	}
}

func TestDispatchDoesNotRetryNonRetryableError(t *testing.T) {
	registry := NewRegistry()
	first := &scriptedConn{err: nonRetryableTestError{}}
	second := &scriptedConn{done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"m"}}, first)
	registry.Register(protocol.Register{NodeID: "node-b", ProviderID: "provider-b", Models: []string{"m"}}, second)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-a", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-b", Models: []string{"m"}, LoadAverage: 10})

	result, err := registry.Dispatch(context.Background(), "req", protocol.InferRequest{Model: "m"}, &collectTestSink{})
	if err == nil {
		t.Fatal("dispatch should return non-retryable error")
	}
	if result.Attempts != 1 || second.calls != 0 {
		t.Fatalf("result = %+v second calls=%d, want no retry", result, second.calls)
	}
}

func TestCooldownSkipsFailedNodeOnNextDispatch(t *testing.T) {
	registry := NewRegistry()
	first := &scriptedConn{err: errors.New("startup failed")}
	second := &scriptedConn{done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"m"}}, first)
	registry.Register(protocol.Register{NodeID: "node-b", ProviderID: "provider-b", Models: []string{"m"}}, second)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-a", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-b", Models: []string{"m"}, LoadAverage: 10})

	if _, err := registry.Dispatch(context.Background(), "req-1", protocol.InferRequest{Model: "m"}, &collectTestSink{}); err != nil {
		t.Fatalf("first dispatch: %v", err)
	}
	first.err = nil
	first.done = protocol.InferDone{FinishReason: "stop"}
	result, err := registry.Dispatch(context.Background(), "req-2", protocol.InferRequest{Model: "m"}, &collectTestSink{})
	if err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	if result.NodeID != "node-b" {
		t.Fatalf("second dispatch node = %s, want node-b while node-a is cooling down", result.NodeID)
	}
	if first.calls != 1 {
		t.Fatalf("node-a calls = %d, want no second call during cooldown", first.calls)
	}

	status := registry.NetworkStatus()
	if status.CooldownNodes != 1 {
		t.Fatalf("cooldown nodes = %d, want 1", status.CooldownNodes)
	}
}

func TestSuccessClearsCooldownState(t *testing.T) {
	registry := NewRegistry()
	conn := &scriptedConn{err: errors.New("startup failed")}
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"m"}}, conn)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-a", Models: []string{"m"}, LoadAverage: 0})

	if _, err := registry.Dispatch(context.Background(), "req-1", protocol.InferRequest{Model: "m"}, &collectTestSink{}); err == nil {
		t.Fatal("first dispatch should fail")
	}
	registry.mu.Lock()
	registry.nodes["node-a"].CooldownUntil = time.Now().Add(-time.Second)
	registry.mu.Unlock()

	conn.err = nil
	conn.done = protocol.InferDone{FinishReason: "stop"}
	if _, err := registry.Dispatch(context.Background(), "req-2", protocol.InferRequest{Model: "m"}, &collectTestSink{}); err != nil {
		t.Fatalf("second dispatch: %v", err)
	}
	node := registry.Snapshot()[0]
	if node.ErrorStreak != 0 || node.InCooldown || node.LastError != "" {
		t.Fatalf("node after success = %+v, want cleared cooldown state", node)
	}
}

func TestDispatchRespectsPrivacyTier(t *testing.T) {
	registry := NewRegistry()
	publicConn := &scriptedConn{done: protocol.InferDone{FinishReason: "stop"}}
	privateConn := &scriptedConn{done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{
		NodeID:       "public-node",
		ProviderID:   "rented-provider",
		Models:       []string{"m"},
		PrivacyMode:  privacy.Public,
		PrivacyTiers: []string{privacy.Public},
	}, publicConn)
	registry.Register(protocol.Register{
		NodeID:       "private-node",
		ProviderID:   "trusted-provider",
		Models:       []string{"m"},
		PrivacyMode:  privacy.Private,
		PrivacyTiers: []string{privacy.Private, privacy.Community, privacy.Public},
	}, privateConn)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "public-node", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "private-node", Models: []string{"m"}, LoadAverage: 10})

	privateResult, err := registry.Dispatch(context.Background(), "private-req", protocol.InferRequest{Model: "m", PrivacyTier: privacy.Private}, &collectTestSink{})
	if err != nil {
		t.Fatalf("private dispatch: %v", err)
	}
	if privateResult.NodeID != "private-node" || publicConn.calls != 0 {
		t.Fatalf("private result = %+v public calls=%d, want trusted private node only", privateResult, publicConn.calls)
	}

	publicResult, err := registry.Dispatch(context.Background(), "public-req", protocol.InferRequest{Model: "m", PrivacyTier: privacy.Public}, &collectTestSink{})
	if err != nil {
		t.Fatalf("public dispatch: %v", err)
	}
	if publicResult.NodeID != "public-node" {
		t.Fatalf("public result = %+v, want cheaper public node", publicResult)
	}
}

func TestDispatchPrivateFailsWhenOnlyPublicNodeAvailable(t *testing.T) {
	registry := NewRegistry()
	conn := &scriptedConn{done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{
		NodeID:      "public-node",
		ProviderID:  "rented-provider",
		Models:      []string{"m"},
		PrivacyMode: privacy.Public,
	}, conn)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "public-node", Models: []string{"m"}})

	if _, err := registry.Dispatch(context.Background(), "private-req", protocol.InferRequest{Model: "m", PrivacyTier: privacy.Private}, &collectTestSink{}); !errors.Is(err, ErrNoNode) {
		t.Fatalf("private dispatch err = %v, want ErrNoNode", err)
	}
	if conn.calls != 0 {
		t.Fatalf("public node calls = %d, want 0 for private request", conn.calls)
	}
}

type scriptedConn struct {
	chunk string
	done  protocol.InferDone
	err   error
	calls int
}

func (s *scriptedConn) SendInference(_ context.Context, _ string, _ protocol.InferRequest, sink StreamSink) (protocol.InferDone, error) {
	s.calls++
	if s.chunk != "" {
		if err := sink.Chunk(s.chunk); err != nil {
			return protocol.InferDone{}, err
		}
	}
	if s.err != nil {
		return protocol.InferDone{}, s.err
	}
	return s.done, nil
}

func (s *scriptedConn) Close() error {
	return nil
}

type collectTestSink struct {
	content string
}

func (s *collectTestSink) Chunk(content string) error {
	s.content += content
	return nil
}

type nonRetryableTestError struct{}

func (nonRetryableTestError) Error() string {
	return "do not retry"
}

func (nonRetryableTestError) Retryable() bool {
	return false
}
