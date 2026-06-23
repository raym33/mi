package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/raym33/mi/internal/privacy"
	"github.com/raym33/mi/internal/protocol"
)

var ErrNoNode = errors.New("no healthy node can serve requested model")

const (
	baseErrorCooldown = 10 * time.Second
	maxErrorCooldown  = 60 * time.Second
)

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
	ProviderID    string
	PublicName    string
	City          string
	PrivacyMode   string
	PrivacyTiers  map[string]bool
	Hostname      string
	Models        map[string]bool
	MaxConcurrent int
	Active        int
	QueueDepth    int
	MemoryFreeMB  uint64
	LoadAverage   float64
	LastSeen      time.Time
	ErrorStreak   int
	CooldownUntil time.Time
	LastError     string
	Conn          NodeConn
}

type NodeView struct {
	ID            string    `json:"id"`
	ProviderID    string    `json:"provider_id,omitempty"`
	PublicName    string    `json:"public_name,omitempty"`
	City          string    `json:"city,omitempty"`
	PrivacyMode   string    `json:"privacy_mode,omitempty"`
	PrivacyTiers  []string  `json:"privacy_tiers,omitempty"`
	Hostname      string    `json:"hostname"`
	Models        []string  `json:"models"`
	MaxConcurrent int       `json:"max_concurrent"`
	Active        int       `json:"active"`
	QueueDepth    int       `json:"queue_depth"`
	MemoryFreeMB  uint64    `json:"memory_free_mb"`
	LoadAverage   float64   `json:"load_average"`
	LastSeen      time.Time `json:"last_seen"`
	Healthy       bool      `json:"healthy"`
	ErrorStreak   int       `json:"error_streak,omitempty"`
	CooldownUntil time.Time `json:"cooldown_until,omitempty"`
	LastError     string    `json:"last_error,omitempty"`
	InCooldown    bool      `json:"in_cooldown"`
}

type NetworkStatus struct {
	Nodes             int      `json:"nodes"`
	HealthyNodes      int      `json:"healthy_nodes"`
	ActiveRequests    int      `json:"active_requests"`
	MaxConcurrent     int      `json:"max_concurrent"`
	AvailableSlots    int      `json:"available_slots"`
	CooldownNodes     int      `json:"cooldown_nodes"`
	TotalMemoryFreeMB uint64   `json:"total_memory_free_mb"`
	Models            []string `json:"models"`
	Cities            []string `json:"cities,omitempty"`
	PrivacyTiers      []string `json:"privacy_tiers,omitempty"`
}

type DispatchResult struct {
	Done       protocol.InferDone
	NodeID     string
	ProviderID string
	Attempts   int
}

type RetryableError interface {
	error
	Retryable() bool
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
	var privacyTiers []string
	var err error
	if len(msg.PrivacyTiers) > 0 {
		privacyTiers, err = privacy.NormalizeTiers(msg.PrivacyTiers)
	} else {
		privacyTiers, err = privacy.TiersForMode(msg.PrivacyMode)
	}
	if err != nil {
		privacyTiers, _ = privacy.TiersForMode(privacy.Private)
	}
	acceptedPrivacy := map[string]bool{}
	for _, tier := range privacyTiers {
		acceptedPrivacy[tier] = true
	}
	r.nodes[msg.NodeID] = &Node{
		ID:            msg.NodeID,
		ProviderID:    msg.ProviderID,
		PublicName:    msg.PublicName,
		City:          msg.City,
		PrivacyMode:   msg.PrivacyMode,
		PrivacyTiers:  acceptedPrivacy,
		Hostname:      msg.Hostname,
		Models:        models,
		MaxConcurrent: max(1, msg.MaxConcurrent),
		LastSeen:      time.Now(),
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

func (r *Registry) RemoveProvider(providerID string) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	removed := 0
	for nodeID, node := range r.nodes {
		if node.ProviderID != providerID {
			continue
		}
		if node.Conn != nil {
			_ = node.Conn.Close()
		}
		delete(r.nodes, nodeID)
		removed++
	}
	return removed
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

func (r *Registry) Snapshot() []NodeView {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]NodeView, 0, len(r.nodes))
	for _, node := range r.nodes {
		models := make([]string, 0, len(node.Models))
		for model := range node.Models {
			models = append(models, model)
		}
		sort.Strings(models)
		privacyTiers := make([]string, 0, len(node.PrivacyTiers))
		for tier := range node.PrivacyTiers {
			privacyTiers = append(privacyTiers, tier)
		}
		sort.Strings(privacyTiers)
		out = append(out, NodeView{
			ID:            node.ID,
			ProviderID:    node.ProviderID,
			PublicName:    node.PublicName,
			City:          node.City,
			PrivacyMode:   node.PrivacyMode,
			PrivacyTiers:  privacyTiers,
			Hostname:      node.Hostname,
			Models:        models,
			MaxConcurrent: node.MaxConcurrent,
			Active:        node.Active,
			QueueDepth:    node.QueueDepth,
			MemoryFreeMB:  node.MemoryFreeMB,
			LoadAverage:   node.LoadAverage,
			LastSeen:      node.LastSeen,
			Healthy:       node.healthy(),
			ErrorStreak:   node.ErrorStreak,
			CooldownUntil: node.CooldownUntil,
			LastError:     node.LastError,
			InCooldown:    node.inCooldown(time.Now()),
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (r *Registry) NetworkStatus() NetworkStatus {
	r.mu.Lock()
	defer r.mu.Unlock()

	models := map[string]bool{}
	cities := map[string]bool{}
	privacyTiers := map[string]bool{}
	status := NetworkStatus{Nodes: len(r.nodes)}
	now := time.Now()
	for _, node := range r.nodes {
		if !node.healthy() {
			continue
		}
		if node.inCooldown(now) {
			status.CooldownNodes++
			continue
		}
		status.HealthyNodes++
		status.ActiveRequests += node.Active
		status.MaxConcurrent += node.MaxConcurrent
		status.TotalMemoryFreeMB += node.MemoryFreeMB
		if node.MaxConcurrent > node.Active {
			status.AvailableSlots += node.MaxConcurrent - node.Active
		}
		for model := range node.Models {
			models[model] = true
		}
		if node.City != "" {
			cities[node.City] = true
		}
		for tier := range node.PrivacyTiers {
			privacyTiers[tier] = true
		}
	}
	for model := range models {
		status.Models = append(status.Models, model)
	}
	for city := range cities {
		status.Cities = append(status.Cities, city)
	}
	for tier := range privacyTiers {
		status.PrivacyTiers = append(status.PrivacyTiers, tier)
	}
	sort.Strings(status.Models)
	sort.Strings(status.Cities)
	sort.Strings(status.PrivacyTiers)
	return status
}

func (r *Registry) Dispatch(ctx context.Context, requestID string, req protocol.InferRequest, sink StreamSink) (DispatchResult, error) {
	return r.dispatch(ctx, requestID, req, sink, "")
}

func (r *Registry) DispatchToProvider(ctx context.Context, requestID string, providerID string, req protocol.InferRequest, sink StreamSink) (DispatchResult, error) {
	return r.dispatch(ctx, requestID, req, sink, providerID)
}

func (r *Registry) dispatch(ctx context.Context, requestID string, req protocol.InferRequest, sink StreamSink, providerID string) (DispatchResult, error) {
	attempted := map[string]bool{}
	var lastErr error
	attempts := 0
	for {
		node, err := r.reserveFiltered(req.Model, req.PrivacyTier, attempted, providerID)
		if err != nil {
			if lastErr != nil {
				return DispatchResult{Attempts: attempts}, fmt.Errorf("%w after %d failed attempt(s): %v", ErrNoNode, attempts, lastErr)
			}
			return DispatchResult{Attempts: attempts}, err
		}
		attempts++
		tracker := &firstChunkTracker{inner: sink}
		done, err := node.Conn.SendInference(ctx, requestID, req, tracker)
		r.release(node.ID)
		if err == nil {
			r.recordSuccess(node.ID)
			return DispatchResult{Done: done, NodeID: node.ID, ProviderID: node.ProviderID, Attempts: attempts}, nil
		}
		if tracker.sent || !canRetry(ctx, err) {
			return DispatchResult{NodeID: node.ID, ProviderID: node.ProviderID, Attempts: attempts}, err
		}
		r.recordPreTokenFailure(node.ID, err)
		attempted[node.ID] = true
		lastErr = err
	}
}

func (r *Registry) reserve(model string, privacyTier string, exclude map[string]bool) (*Node, error) {
	return r.reserveFiltered(model, privacyTier, exclude, "")
}

func (r *Registry) reserveFiltered(model string, privacyTier string, exclude map[string]bool, providerID string) (*Node, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var candidates []*Node
	now := time.Now()
	for _, node := range r.nodes {
		if exclude[node.ID] {
			continue
		}
		if providerID != "" && node.ProviderID != providerID {
			continue
		}
		if node.healthy() && !node.inCooldown(now) && node.Models[model] && node.acceptsPrivacy(privacyTier) && node.Active < node.MaxConcurrent {
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

func (r *Registry) recordSuccess(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if node, ok := r.nodes[nodeID]; ok {
		node.ErrorStreak = 0
		node.CooldownUntil = time.Time{}
		node.LastError = ""
	}
}

func (r *Registry) recordPreTokenFailure(nodeID string, err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok {
		return
	}
	node.ErrorStreak++
	cooldown := baseErrorCooldown * time.Duration(node.ErrorStreak)
	if cooldown > maxErrorCooldown {
		cooldown = maxErrorCooldown
	}
	node.CooldownUntil = time.Now().Add(cooldown)
	node.LastError = err.Error()
}

func (n *Node) healthy() bool {
	return n.Conn != nil && time.Since(n.LastSeen) < 20*time.Second
}

func (n *Node) inCooldown(now time.Time) bool {
	return !n.CooldownUntil.IsZero() && now.Before(n.CooldownUntil)
}

func (n *Node) acceptsPrivacy(tier string) bool {
	tier, err := privacy.NormalizeTier(tier)
	if err != nil {
		return false
	}
	if len(n.PrivacyTiers) == 0 {
		return tier == privacy.Private
	}
	return n.PrivacyTiers[tier]
}

func cost(n *Node) float64 {
	return float64(n.Active*10+n.QueueDepth*5) + n.LoadAverage
}

type firstChunkTracker struct {
	inner StreamSink
	sent  bool
}

func (s *firstChunkTracker) Chunk(content string) error {
	s.sent = true
	return s.inner.Chunk(content)
}

func canRetry(ctx context.Context, err error) bool {
	if err == nil {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	var retryable RetryableError
	if errors.As(err, &retryable) {
		return retryable.Retryable()
	}
	return true
}
