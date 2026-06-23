package scheduler

import (
	"context"
	"errors"
	"testing"

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
