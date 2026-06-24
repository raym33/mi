package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
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
	mu             sync.Mutex
	nodes          map[string]*Node
	providerScores map[string]int
}

type Node struct {
	ID                      string
	ProviderID              string
	ProtocolVersion         int
	PublicName              string
	City                    string
	PrivacyMode             string
	PrivacyTiers            map[string]bool
	Hostname                string
	Backend                 string
	DeviceKind              string
	DeviceVendor            string
	DeviceModel             string
	SoC                     string
	Accelerators            map[string]bool
	PowerMode               string
	NetworkMode             string
	Models                  map[string]bool
	MaxConcurrent           int
	Active                  int
	QueueDepth              int
	MemoryFreeMB            uint64
	LoadAverage             float64
	LastSeen                time.Time
	ErrorStreak             int
	CooldownUntil           time.Time
	LastError               string
	CompletedRequests       int64
	FailedRequests          int64
	PreTokenFailures        int64
	PostTokenFailures       int64
	ObservedLatencyMS       float64
	ObservedTTFTMS          float64
	ObservedTokensPerSecond float64
	LastSuccessAt           time.Time
	LastFailureAt           time.Time
	Conn                    NodeConn
}

type NodeView struct {
	ID                      string    `json:"id"`
	ProviderID              string    `json:"provider_id,omitempty"`
	ProtocolVersion         int       `json:"protocol_version,omitempty"`
	PublicName              string    `json:"public_name,omitempty"`
	City                    string    `json:"city,omitempty"`
	PrivacyMode             string    `json:"privacy_mode,omitempty"`
	PrivacyTiers            []string  `json:"privacy_tiers,omitempty"`
	Hostname                string    `json:"hostname"`
	Backend                 string    `json:"backend,omitempty"`
	DeviceKind              string    `json:"device_kind,omitempty"`
	DeviceVendor            string    `json:"device_vendor,omitempty"`
	DeviceModel             string    `json:"device_model,omitempty"`
	SoC                     string    `json:"soc,omitempty"`
	Accelerators            []string  `json:"accelerators,omitempty"`
	PowerMode               string    `json:"power_mode,omitempty"`
	NetworkMode             string    `json:"network_mode,omitempty"`
	Models                  []string  `json:"models"`
	MaxConcurrent           int       `json:"max_concurrent"`
	Active                  int       `json:"active"`
	QueueDepth              int       `json:"queue_depth"`
	MemoryFreeMB            uint64    `json:"memory_free_mb"`
	LoadAverage             float64   `json:"load_average"`
	LastSeen                time.Time `json:"last_seen"`
	Healthy                 bool      `json:"healthy"`
	ErrorStreak             int       `json:"error_streak,omitempty"`
	CooldownUntil           time.Time `json:"cooldown_until,omitempty"`
	LastError               string    `json:"last_error,omitempty"`
	InCooldown              bool      `json:"in_cooldown"`
	ProviderScore           int       `json:"provider_score,omitempty"`
	CompletedRequests       int64     `json:"completed_requests,omitempty"`
	FailedRequests          int64     `json:"failed_requests,omitempty"`
	PreTokenFailures        int64     `json:"pre_token_failures,omitempty"`
	PostTokenFailures       int64     `json:"post_token_failures,omitempty"`
	FailureRateBPS          int64     `json:"failure_rate_bps,omitempty"`
	ObservedLatencyMs       int64     `json:"observed_latency_ms,omitempty"`
	ObservedTTFTMs          int64     `json:"observed_ttft_ms,omitempty"`
	ObservedTokensPerSecond float64   `json:"observed_tokens_per_second,omitempty"`
	LastSuccessAt           time.Time `json:"last_success_at,omitempty"`
	LastFailureAt           time.Time `json:"last_failure_at,omitempty"`
}

type NetworkStatus struct {
	Nodes                  int      `json:"nodes"`
	HealthyNodes           int      `json:"healthy_nodes"`
	ActiveRequests         int      `json:"active_requests"`
	MaxConcurrent          int      `json:"max_concurrent"`
	AvailableSlots         int      `json:"available_slots"`
	CooldownNodes          int      `json:"cooldown_nodes"`
	TotalMemoryFreeMB      uint64   `json:"total_memory_free_mb"`
	Models                 []string `json:"models"`
	Cities                 []string `json:"cities,omitempty"`
	PrivacyTiers           []string `json:"privacy_tiers,omitempty"`
	Backends               []string `json:"backends,omitempty"`
	DeviceKinds            []string `json:"device_kinds,omitempty"`
	Accelerators           []string `json:"accelerators,omitempty"`
	SoCs                   []string `json:"socs,omitempty"`
	CompletedRequests      int64    `json:"completed_requests,omitempty"`
	FailedRequests         int64    `json:"failed_requests,omitempty"`
	AverageLatencyMs       int64    `json:"average_latency_ms,omitempty"`
	AverageTTFTMs          int64    `json:"average_ttft_ms,omitempty"`
	AverageTokensPerSecond float64  `json:"average_tokens_per_second,omitempty"`
}

type DispatchResult struct {
	Done            protocol.InferDone
	NodeID          string
	ProviderID      string
	Backend         string
	DeviceKind      string
	Accelerators    []string
	Attempts        int
	LatencyMs       int64
	TTFTMs          int64
	OutputTokens    int
	TokensPerSecond float64
}

type RetryableError interface {
	error
	Retryable() bool
}

func NewRegistry() *Registry {
	return &Registry{nodes: map[string]*Node{}, providerScores: map[string]int{}}
}

func (r *Registry) SetProviderScores(scores map[string]int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.providerScores = map[string]int{}
	for providerID, score := range scores {
		r.providerScores[providerID] = clampProviderScore(score)
	}
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
	accelerators := map[string]bool{}
	for _, accelerator := range msg.Accelerators {
		if accelerator != "" {
			accelerators[accelerator] = true
		}
	}
	r.nodes[msg.NodeID] = &Node{
		ID:              msg.NodeID,
		ProviderID:      msg.ProviderID,
		ProtocolVersion: normalizeProtocolVersion(msg.ProtocolVersion),
		PublicName:      msg.PublicName,
		City:            msg.City,
		PrivacyMode:     msg.PrivacyMode,
		PrivacyTiers:    acceptedPrivacy,
		Hostname:        msg.Hostname,
		Backend:         msg.Backend,
		DeviceKind:      msg.DeviceKind,
		DeviceVendor:    msg.DeviceVendor,
		DeviceModel:     msg.DeviceModel,
		SoC:             msg.SoC,
		Accelerators:    accelerators,
		PowerMode:       msg.PowerMode,
		NetworkMode:     msg.NetworkMode,
		Models:          models,
		MaxConcurrent:   max(1, msg.MaxConcurrent),
		LastSeen:        time.Now(),
		Conn:            conn,
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
	if msg.ProtocolVersion > 0 {
		node.ProtocolVersion = normalizeProtocolVersion(msg.ProtocolVersion)
	}
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
		accelerators := make([]string, 0, len(node.Accelerators))
		for accelerator := range node.Accelerators {
			accelerators = append(accelerators, accelerator)
		}
		sort.Strings(accelerators)
		out = append(out, NodeView{
			ID:                      node.ID,
			ProviderID:              node.ProviderID,
			ProtocolVersion:         node.ProtocolVersion,
			PublicName:              node.PublicName,
			City:                    node.City,
			PrivacyMode:             node.PrivacyMode,
			PrivacyTiers:            privacyTiers,
			Hostname:                node.Hostname,
			Backend:                 node.Backend,
			DeviceKind:              node.DeviceKind,
			DeviceVendor:            node.DeviceVendor,
			DeviceModel:             node.DeviceModel,
			SoC:                     node.SoC,
			Accelerators:            accelerators,
			PowerMode:               node.PowerMode,
			NetworkMode:             node.NetworkMode,
			Models:                  models,
			MaxConcurrent:           node.MaxConcurrent,
			Active:                  node.Active,
			QueueDepth:              node.QueueDepth,
			MemoryFreeMB:            node.MemoryFreeMB,
			LoadAverage:             node.LoadAverage,
			LastSeen:                node.LastSeen,
			Healthy:                 node.healthy(),
			ErrorStreak:             node.ErrorStreak,
			CooldownUntil:           node.CooldownUntil,
			LastError:               node.LastError,
			InCooldown:              node.inCooldown(time.Now()),
			ProviderScore:           r.providerScoreLocked(node.ProviderID),
			CompletedRequests:       node.CompletedRequests,
			FailedRequests:          node.FailedRequests,
			PreTokenFailures:        node.PreTokenFailures,
			PostTokenFailures:       node.PostTokenFailures,
			FailureRateBPS:          node.failureRateBPS(),
			ObservedLatencyMs:       int64(node.ObservedLatencyMS),
			ObservedTTFTMs:          int64(node.ObservedTTFTMS),
			ObservedTokensPerSecond: roundFloat(node.ObservedTokensPerSecond),
			LastSuccessAt:           node.LastSuccessAt,
			LastFailureAt:           node.LastFailureAt,
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
	backends := map[string]bool{}
	deviceKinds := map[string]bool{}
	accelerators := map[string]bool{}
	socs := map[string]bool{}
	status := NetworkStatus{Nodes: len(r.nodes)}
	observedLatencyNodes := 0
	observedTTFTNodes := 0
	observedTPSNodes := 0
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
		status.CompletedRequests += node.CompletedRequests
		status.FailedRequests += node.FailedRequests
		if node.ObservedLatencyMS > 0 {
			status.AverageLatencyMs += int64(node.ObservedLatencyMS)
			observedLatencyNodes++
		}
		if node.ObservedTTFTMS > 0 {
			status.AverageTTFTMs += int64(node.ObservedTTFTMS)
			observedTTFTNodes++
		}
		if node.ObservedTokensPerSecond > 0 {
			status.AverageTokensPerSecond += node.ObservedTokensPerSecond
			observedTPSNodes++
		}
		if node.MaxConcurrent > node.Active {
			status.AvailableSlots += node.MaxConcurrent - node.Active
		}
		for model := range node.Models {
			models[model] = true
		}
		if node.City != "" {
			cities[node.City] = true
		}
		if node.Backend != "" {
			backends[node.Backend] = true
		}
		if node.DeviceKind != "" {
			deviceKinds[node.DeviceKind] = true
		}
		if node.SoC != "" {
			socs[node.SoC] = true
		}
		for tier := range node.PrivacyTiers {
			privacyTiers[tier] = true
		}
		for accelerator := range node.Accelerators {
			accelerators[accelerator] = true
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
	for backend := range backends {
		status.Backends = append(status.Backends, backend)
	}
	for deviceKind := range deviceKinds {
		status.DeviceKinds = append(status.DeviceKinds, deviceKind)
	}
	for accelerator := range accelerators {
		status.Accelerators = append(status.Accelerators, accelerator)
	}
	for soc := range socs {
		status.SoCs = append(status.SoCs, soc)
	}
	sort.Strings(status.Models)
	sort.Strings(status.Cities)
	sort.Strings(status.PrivacyTiers)
	sort.Strings(status.Backends)
	sort.Strings(status.DeviceKinds)
	sort.Strings(status.Accelerators)
	sort.Strings(status.SoCs)
	if observedLatencyNodes > 0 {
		status.AverageLatencyMs /= int64(observedLatencyNodes)
	}
	if observedTTFTNodes > 0 {
		status.AverageTTFTMs /= int64(observedTTFTNodes)
	}
	if observedTPSNodes > 0 {
		status.AverageTokensPerSecond = roundFloat(status.AverageTokensPerSecond / float64(observedTPSNodes))
	}
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
		node, err := r.reserveFiltered(req, attempted, providerID)
		if err != nil {
			if lastErr != nil {
				return DispatchResult{Attempts: attempts}, fmt.Errorf("%w after %d failed attempt(s): %v", ErrNoNode, attempts, lastErr)
			}
			return DispatchResult{Attempts: attempts}, err
		}
		attempts++
		tracker := newFirstChunkTracker(sink)
		done, err := node.Conn.SendInference(ctx, requestID, req, tracker)
		observation := tracker.observation()
		r.release(node.ID)
		if err == nil {
			r.recordSuccess(node.ID, observation)
			return nodeDispatchResult(node, done, attempts, observation), nil
		}
		if tracker.sent {
			r.recordPostTokenFailure(node.ID, err, observation)
			return nodeDispatchResult(node, protocol.InferDone{}, attempts, observation), err
		}
		if !canRetry(ctx, err) {
			r.recordPreTokenTerminalFailure(node.ID, err, observation)
			return nodeDispatchResult(node, protocol.InferDone{}, attempts, observation), err
		}
		r.recordPreTokenFailure(node.ID, err, observation)
		attempted[node.ID] = true
		lastErr = err
	}
}

func (r *Registry) reserve(req protocol.InferRequest, exclude map[string]bool) (*Node, error) {
	return r.reserveFiltered(req, exclude, "")
}

func (r *Registry) reserveFiltered(req protocol.InferRequest, exclude map[string]bool, providerID string) (*Node, error) {
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
		if node.healthy() && !node.inCooldown(now) && node.Models[req.Model] && node.acceptsPrivacy(req.PrivacyTier) && node.matchesCapabilities(req) && node.Active < node.MaxConcurrent {
			candidates = append(candidates, node)
		}
	}
	if len(candidates) == 0 {
		return nil, ErrNoNode
	}
	sort.Slice(candidates, func(i, j int) bool {
		return r.nodeCostLocked(candidates[i]) < r.nodeCostLocked(candidates[j])
	})
	selected := candidates[0]
	selected.Active++
	return selected, nil
}

func nodeDispatchResult(node *Node, done protocol.InferDone, attempts int, observation dispatchObservation) DispatchResult {
	accelerators := make([]string, 0, len(node.Accelerators))
	for accelerator := range node.Accelerators {
		accelerators = append(accelerators, accelerator)
	}
	sort.Strings(accelerators)
	return DispatchResult{
		Done:            done,
		NodeID:          node.ID,
		ProviderID:      node.ProviderID,
		Backend:         node.Backend,
		DeviceKind:      node.DeviceKind,
		Accelerators:    accelerators,
		Attempts:        attempts,
		LatencyMs:       observation.Latency.Milliseconds(),
		TTFTMs:          observation.TTFT.Milliseconds(),
		OutputTokens:    observation.OutputTokens,
		TokensPerSecond: roundFloat(observation.TokensPerSecond),
	}
}

func (n *Node) matchesCapabilities(req protocol.InferRequest) bool {
	if req.Backend != "" && !strings.EqualFold(n.Backend, req.Backend) {
		return false
	}
	if req.DeviceKind != "" && !strings.EqualFold(n.DeviceKind, req.DeviceKind) {
		return false
	}
	if req.SoC != "" && !strings.EqualFold(n.SoC, req.SoC) {
		return false
	}
	for _, required := range req.Accelerators {
		if required == "" {
			continue
		}
		if !n.hasAccelerator(required) {
			return false
		}
	}
	return true
}

func (n *Node) hasAccelerator(required string) bool {
	for accelerator := range n.Accelerators {
		if strings.EqualFold(accelerator, required) {
			return true
		}
	}
	return false
}

func (r *Registry) release(nodeID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if node, ok := r.nodes[nodeID]; ok && node.Active > 0 {
		node.Active--
	}
}

func (r *Registry) recordSuccess(nodeID string, observation dispatchObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if node, ok := r.nodes[nodeID]; ok {
		node.ErrorStreak = 0
		node.CooldownUntil = time.Time{}
		node.LastError = ""
		node.CompletedRequests++
		node.LastSuccessAt = time.Now()
		node.recordObservation(observation)
	}
}

func (r *Registry) recordPreTokenFailure(nodeID string, err error, observation dispatchObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok {
		return
	}
	node.ErrorStreak++
	node.FailedRequests++
	node.PreTokenFailures++
	cooldown := baseErrorCooldown * time.Duration(node.ErrorStreak)
	if cooldown > maxErrorCooldown {
		cooldown = maxErrorCooldown
	}
	node.CooldownUntil = time.Now().Add(cooldown)
	node.LastError = err.Error()
	node.LastFailureAt = time.Now()
	node.recordObservation(observation)
}

func (r *Registry) recordPostTokenFailure(nodeID string, err error, observation dispatchObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok {
		return
	}
	node.FailedRequests++
	node.PostTokenFailures++
	node.LastError = err.Error()
	node.LastFailureAt = time.Now()
	node.recordObservation(observation)
}

func (r *Registry) recordPreTokenTerminalFailure(nodeID string, err error, observation dispatchObservation) {
	r.mu.Lock()
	defer r.mu.Unlock()
	node, ok := r.nodes[nodeID]
	if !ok {
		return
	}
	node.FailedRequests++
	node.PreTokenFailures++
	node.LastError = err.Error()
	node.LastFailureAt = time.Now()
	node.recordObservation(observation)
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

func (r *Registry) nodeCostLocked(n *Node) float64 {
	utilization := 0.0
	if n.MaxConcurrent > 0 {
		utilization = float64(n.Active) / float64(n.MaxConcurrent)
	}
	queuePenalty := float64(n.QueueDepth) * 5
	loadPenalty := n.LoadAverage
	errorPenalty := float64(n.ErrorStreak) * 8
	reputationPenalty := float64(100-r.providerScoreLocked(n.ProviderID)) / 5
	latencyPenalty := minFloat(n.ObservedLatencyMS/1000, 20)
	ttftPenalty := minFloat(n.ObservedTTFTMS/500, 10)
	throughputPenalty := 0.0
	if n.ObservedTokensPerSecond > 0 {
		throughputPenalty = maxFloat(0, 10-minFloat(n.ObservedTokensPerSecond, 10))
	}
	failurePenalty := float64(n.failureRateBPS()) / 10000 * 20
	return utilization*20 + queuePenalty + loadPenalty + errorPenalty + reputationPenalty + latencyPenalty + ttftPenalty + throughputPenalty + failurePenalty
}

func (r *Registry) providerScoreLocked(providerID string) int {
	if providerID == "" {
		return 70
	}
	score, ok := r.providerScores[providerID]
	if !ok {
		return 70
	}
	return clampProviderScore(score)
}

func clampProviderScore(score int) int {
	if score < 0 {
		return 0
	}
	if score > 100 {
		return 100
	}
	return score
}

func normalizeProtocolVersion(version int) int {
	if version <= 0 {
		return protocol.Version
	}
	return version
}

type dispatchObservation struct {
	Latency         time.Duration
	TTFT            time.Duration
	OutputTokens    int
	TokensPerSecond float64
}

type firstChunkTracker struct {
	inner        StreamSink
	startedAt    time.Time
	firstChunkAt time.Time
	sent         bool
	outputChars  int
}

func newFirstChunkTracker(inner StreamSink) *firstChunkTracker {
	return &firstChunkTracker{inner: inner, startedAt: time.Now()}
}

func (s *firstChunkTracker) Chunk(content string) error {
	if !s.sent {
		s.sent = true
		s.firstChunkAt = time.Now()
	}
	s.outputChars += len(content)
	return s.inner.Chunk(content)
}

func (s *firstChunkTracker) observation() dispatchObservation {
	latency := time.Since(s.startedAt)
	ttft := time.Duration(0)
	if s.sent {
		ttft = s.firstChunkAt.Sub(s.startedAt)
	}
	outputTokens := estimateTokensFromChars(s.outputChars)
	return dispatchObservation{
		Latency:         latency,
		TTFT:            ttft,
		OutputTokens:    outputTokens,
		TokensPerSecond: observedTokensPerSecond(outputTokens, latency),
	}
}

func (n *Node) recordObservation(observation dispatchObservation) {
	if observation.Latency > 0 {
		n.ObservedLatencyMS = ewma(n.ObservedLatencyMS, float64(observation.Latency.Milliseconds()))
	}
	if observation.TTFT > 0 {
		n.ObservedTTFTMS = ewma(n.ObservedTTFTMS, float64(observation.TTFT.Milliseconds()))
	}
	if observation.TokensPerSecond > 0 {
		n.ObservedTokensPerSecond = ewma(n.ObservedTokensPerSecond, observation.TokensPerSecond)
	}
}

func (n *Node) failureRateBPS() int64 {
	total := n.CompletedRequests + n.FailedRequests
	if total <= 0 {
		return 0
	}
	return n.FailedRequests * 10000 / total
}

func estimateTokensFromChars(chars int) int {
	if chars <= 0 {
		return 0
	}
	return (chars + 3) / 4
}

func observedTokensPerSecond(tokens int, latency time.Duration) float64 {
	if tokens <= 0 || latency <= 0 {
		return 0
	}
	return float64(tokens) / latency.Seconds()
}

func ewma(current float64, sample float64) float64 {
	if sample <= 0 {
		return current
	}
	if current <= 0 {
		return sample
	}
	return current*0.8 + sample*0.2
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

func maxFloat(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

func roundFloat(value float64) float64 {
	return float64(int(value*100)) / 100
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
