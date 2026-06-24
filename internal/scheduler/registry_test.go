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

func (f *fakeConn) SendEmbedding(context.Context, string, protocol.EmbedRequest) (protocol.EmbedResult, error) {
	return protocol.EmbedResult{Vectors: [][]float32{{1}}, PromptTokens: 1}, nil
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

func TestRegisterExposesBackendAndHardwareMetadata(t *testing.T) {
	registry := NewRegistry()
	registry.Register(protocol.Register{
		ProtocolVersion: protocol.Version,
		NodeID:          "xiaomi-node",
		ProviderID:      "provider-a",
		Backend:         "ort-qnn",
		DeviceKind:      "android",
		DeviceVendor:    "xiaomi",
		DeviceModel:     "xiaomi_15_ultra",
		SoC:             "snapdragon_8_elite",
		Accelerators:    []string{"adreno", "hexagon_npu"},
		Models:          []string{"llama3.2-3b-qnn"},
	}, &fakeConn{})

	nodes := registry.Snapshot()
	if len(nodes) != 1 {
		t.Fatalf("nodes = %+v, want one node", nodes)
	}
	node := nodes[0]
	if node.Backend != "ort-qnn" || node.DeviceKind != "android" || node.DeviceVendor != "xiaomi" || node.SoC != "snapdragon_8_elite" {
		t.Fatalf("node metadata = %+v, want android/xiaomi/qnn", node)
	}
	if node.ProtocolVersion != protocol.Version {
		t.Fatalf("protocol version = %d, want %d", node.ProtocolVersion, protocol.Version)
	}
	if len(node.Accelerators) != 2 || node.Accelerators[0] != "adreno" || node.Accelerators[1] != "hexagon_npu" {
		t.Fatalf("accelerators = %+v, want sorted accelerator list", node.Accelerators)
	}
	status := registry.NetworkStatus()
	if len(status.Backends) != 1 || status.Backends[0] != "ort-qnn" || len(status.DeviceKinds) != 1 || status.DeviceKinds[0] != "android" {
		t.Fatalf("status = %+v, want backend and device kind", status)
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

func TestDispatchFiltersBackendAndAccelerators(t *testing.T) {
	registry := NewRegistry()
	metal := &scriptedConn{chunk: "metal", done: protocol.InferDone{FinishReason: "stop"}}
	cuda := &scriptedConn{chunk: "cuda", done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{
		NodeID:       "mac-node",
		ProviderID:   "provider-mac",
		Backend:      "mlx",
		DeviceKind:   "mac",
		Accelerators: []string{"metal"},
		Models:       []string{"m"},
	}, metal)
	registry.Register(protocol.Register{
		NodeID:       "cuda-node",
		ProviderID:   "provider-cuda",
		Backend:      "vllm",
		DeviceKind:   "server",
		Accelerators: []string{"cuda"},
		Models:       []string{"m"},
	}, cuda)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "mac-node", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "cuda-node", Models: []string{"m"}, LoadAverage: 10})

	sink := &collectTestSink{}
	result, err := registry.Dispatch(context.Background(), "req", protocol.InferRequest{Model: "m", Backend: "VLLM", Accelerators: []string{"CUDA"}}, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.NodeID != "cuda-node" || result.Backend != "vllm" || sink.content != "cuda" || metal.calls != 0 {
		t.Fatalf("result = %+v content=%q metal calls=%d, want cuda node only", result, sink.content, metal.calls)
	}
}

func TestProviderScoresInfluenceDispatch(t *testing.T) {
	registry := NewRegistry()
	shaky := &scriptedConn{chunk: "shaky", done: protocol.InferDone{FinishReason: "stop"}}
	trusted := &scriptedConn{chunk: "trusted", done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{NodeID: "node-shaky", ProviderID: "provider-shaky", Models: []string{"m"}}, shaky)
	registry.Register(protocol.Register{NodeID: "node-trusted", ProviderID: "provider-trusted", Models: []string{"m"}}, trusted)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-shaky", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-trusted", Models: []string{"m"}, LoadAverage: 10})
	registry.SetProviderScores(map[string]int{
		"provider-shaky":   0,
		"provider-trusted": 100,
	})

	sink := &collectTestSink{}
	result, err := registry.Dispatch(context.Background(), "req", protocol.InferRequest{Model: "m"}, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.NodeID != "node-trusted" || sink.content != "trusted" {
		t.Fatalf("result = %+v content=%q, want higher-reputation provider despite higher load", result, sink.content)
	}
	if shaky.calls != 0 || trusted.calls != 1 {
		t.Fatalf("calls shaky=%d trusted=%d, want only trusted", shaky.calls, trusted.calls)
	}
}

func TestDispatchRecordsObservedMetrics(t *testing.T) {
	registry := NewRegistry()
	conn := &scriptedConn{chunk: "abcdefgh", done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"m"}}, conn)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-a", Models: []string{"m"}})

	result, err := registry.Dispatch(context.Background(), "req", protocol.InferRequest{Model: "m"}, &collectTestSink{})
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.OutputTokens != 2 || result.TokensPerSecond <= 0 {
		t.Fatalf("result metrics = %+v, want estimated output tokens and throughput", result)
	}
	node := registry.Snapshot()[0]
	if node.CompletedRequests != 1 || node.FailedRequests != 0 || node.ObservedTokensPerSecond <= 0 {
		t.Fatalf("node metrics = %+v, want one completed request with throughput", node)
	}
	status := registry.NetworkStatus()
	if status.CompletedRequests != 1 || status.AverageTokensPerSecond <= 0 {
		t.Fatalf("status metrics = %+v, want completed request and throughput", status)
	}
}

func TestObservedPerformanceInfluencesDispatch(t *testing.T) {
	registry := NewRegistry()
	slow := &scriptedConn{chunk: "slow", done: protocol.InferDone{FinishReason: "stop"}}
	fast := &scriptedConn{chunk: "fast", done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{NodeID: "node-slow", ProviderID: "provider-slow", Models: []string{"m"}}, slow)
	registry.Register(protocol.Register{NodeID: "node-fast", ProviderID: "provider-fast", Models: []string{"m"}}, fast)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-slow", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-fast", Models: []string{"m"}, LoadAverage: 10})
	registry.recordSuccess("node-slow", dispatchObservation{Latency: 3 * time.Second, TTFT: 2 * time.Second, OutputTokens: 10, TokensPerSecond: 1})
	registry.recordSuccess("node-fast", dispatchObservation{Latency: 100 * time.Millisecond, TTFT: 50 * time.Millisecond, OutputTokens: 100, TokensPerSecond: 50})

	sink := &collectTestSink{}
	result, err := registry.Dispatch(context.Background(), "req", protocol.InferRequest{Model: "m"}, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.NodeID != "node-fast" || sink.content != "fast" {
		t.Fatalf("result = %+v content=%q, want observed fast node despite higher load", result, sink.content)
	}
}

func TestFailuresDoNotImproveObservedLatency(t *testing.T) {
	registry := NewRegistry()
	conn := &scriptedConn{err: nonRetryableTestError{}}
	registry.Register(protocol.Register{NodeID: "node-a", ProviderID: "provider-a", Models: []string{"m"}}, conn)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-a", Models: []string{"m"}})
	registry.recordSuccess("node-a", dispatchObservation{Latency: 5 * time.Second, TTFT: 3 * time.Second, OutputTokens: 10, TokensPerSecond: 2})

	if _, err := registry.Dispatch(context.Background(), "req", protocol.InferRequest{Model: "m"}, &collectTestSink{}); err == nil {
		t.Fatal("dispatch should fail")
	}
	node := registry.Snapshot()[0]
	if node.ObservedLatencyMs != 5000 || node.ObservedTTFTMs != 3000 {
		t.Fatalf("observed metrics after failure = latency %d ttft %d, want unchanged 5000/3000", node.ObservedLatencyMs, node.ObservedTTFTMs)
	}
	if node.FailedRequests != 1 || node.PreTokenFailures != 1 {
		t.Fatalf("failure counters = %+v, want one pre-token failure", node)
	}
}

func TestColdStartPenaltyAvoidsFreshNodeMonopoly(t *testing.T) {
	registry := NewRegistry()
	fresh := &scriptedConn{chunk: "fresh", done: protocol.InferDone{FinishReason: "stop"}}
	proven := &scriptedConn{chunk: "proven", done: protocol.InferDone{FinishReason: "stop"}}
	registry.Register(protocol.Register{NodeID: "node-fresh", ProviderID: "provider-fresh", Models: []string{"m"}}, fresh)
	registry.Register(protocol.Register{NodeID: "node-proven", ProviderID: "provider-proven", Models: []string{"m"}}, proven)
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-fresh", Models: []string{"m"}, LoadAverage: 0})
	registry.Heartbeat(protocol.Heartbeat{NodeID: "node-proven", Models: []string{"m"}, LoadAverage: 5})
	registry.recordSuccess("node-proven", dispatchObservation{Latency: 100 * time.Millisecond, TTFT: 50 * time.Millisecond, OutputTokens: 100, TokensPerSecond: 50})

	sink := &collectTestSink{}
	result, err := registry.Dispatch(context.Background(), "req", protocol.InferRequest{Model: "m"}, sink)
	if err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if result.NodeID != "node-proven" || sink.content != "proven" {
		t.Fatalf("result = %+v content=%q, want proven node over fresh cold-start node", result, sink.content)
	}
	if fresh.calls != 0 || proven.calls != 1 {
		t.Fatalf("calls fresh=%d proven=%d, want only proven", fresh.calls, proven.calls)
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

func (s *scriptedConn) SendEmbedding(_ context.Context, _ string, _ protocol.EmbedRequest) (protocol.EmbedResult, error) {
	s.calls++
	if s.err != nil {
		return protocol.EmbedResult{}, s.err
	}
	return protocol.EmbedResult{Vectors: [][]float32{{1}}, PromptTokens: 1}, nil
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
