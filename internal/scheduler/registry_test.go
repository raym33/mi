package scheduler

import (
	"context"
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
