package scheduler

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/raym33/mi/internal/protocol"
)

var ErrNoNode = errors.New("no healthy node can serve requested model")

type NodeConn interface {
	SendInference(ctx context.Context, requestID string, req protocol.InferRequest, sink StreamSink) (protocol.InferDone, error)
	Close() error
}

type StreamSink interface {
	Chunk(content string) error
}

type Registry struct {
	mu    sync.Mutex
	nodes map[string]*Node
}

type Node struct {
	ID            string
	Hostname      string
	Models        map[string]bool
	MaxConcurrent int
	Active        int
	QueueDepth    int
	MemoryFreeMB  uint64
	LoadAverage   float64
	LastSeen       time.Time
	Conn          NodeConn
}

func NewRegistry() *Registry {
	return &Registry{nodes: map[string]*Node{}}
}

func (r *Registry) Register(msg protocol.Register, conn NodeConn) {
	r.mu.Lock()
	defer r.mu.Unlock()

	models := map[string]bool{}
	for _, model := range msg.Models {
		models[model] = true
	}
	r.nodes[msg.NodeID] = &Node{
		ID:            msg.NodeID,
		Hostname:      msg.Hostname,
		Models:        models,
		MaxConcurrent: max(1, msg.MaxConcurrent),
		LastSeen:       time.Now(),
		Conn:          conn,
	}
}

func (r *Registry) Heartbeat(msg protocol.Heartbeat) {
	r.mu.Lock()
	defer r.mu.Unlock()

	node, ok := r.nodes[msg.NodeID]
	if !ok {
		return
	}
	models := map[string]bool{}
	for _, model := range msg.Models {
		models[model] = true
	}
	node.Models = models
	node.Active = msg.ActiveRequests
	node.QueueDepth = msg.QueueDepth
	node.MemoryFreeMB = msg.MemoryFreeMB
	node.LoadAverage = msg.LoadAverage
	node.LastSeen = time.Now()
}

func (r *Registry) Remove(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if node, ok := r.nodes[nodeID]; ok && node.Conn != nil {
		_ = node.Conn.Close()
	}
	delete(r.nodes, nodeID)
}

func (r *Registry) Models() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	seen := map[string]bool{}
	for _, node := range r.nodes {
		if !node.healthy() {
			continue
		}
		for model := range node.Models {
			seen[model] = true
		}
	}
	models := make([]string, 0, len(seen))
	for model := range seen {
		models = append(models, model)
	}
	sort.Strings(models)
	return models
}

func (r *Registry) Snapshot() []Node {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Node, 0, len(r.nodes))
	for _, node := range r.nodes {
		copyNode := *node
		copyNode.Conn = nil
		out = append(out, copyNode)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) Dispatch(ctx context.Context, requestID string, req protocol.InferRequest, sink StreamSink) (protocol.InferDone, error) {
	node, err := r.reserve(req.Model)
	if err != nil {
		return protocol.InferDone{}, err
	}
	defer r.release(node.ID)
	return node.Conn.SendInference(ctx, requestID, req, sink)
}

func (r *Registry) reserve(model string) (*Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var candidates []*Node
	for _, node := range r.nodes {
		if node.healthy() && node.Models[model] && node.Active < node.MaxConcurrent {
			candidates = append(candidates, node)
		}
	}
	if len(candidates) == 0 {
		return nil, ErrNoNode
	}
	sort.Slice(candidates, func(i, j int) bool {
		return cost(candidates[i]) < cost(candidates[j])
	})
	selected := candidates[0]
	selected.Active++
	return selected, nil
}

func (r *Registry) release(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if node, ok := r.nodes[nodeID]; ok && node.Active > 0 {
		node.Active--
	}
}

func (n *Node) healthy() bool {
	return n.Conn != nil && time.Since(n.LastSeen) < 20*time.Second
}

func cost(n *Node) float64 {
	return float64(n.Active*10+n.QueueDepth*5) + n.LoadAverage
}
