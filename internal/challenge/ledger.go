package challenge

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/raym33/mi/internal/config"
)

const genesisHash = "genesis"

var ErrInvalidChain = errors.New("invalid challenge chain")
var ErrDisabled = errors.New("challenge ledger disabled")

type Ledger struct {
	mu       sync.Mutex
	enabled  bool
	path     string
	events   []Event
	lastHash string
}

type RecordInput struct {
	ProviderID string `json:"provider_id"`
	NodeID     string `json:"node_id,omitempty"`
	Challenge  string `json:"challenge"`
	Passed     bool   `json:"passed"`
	LatencyMs  int64  `json:"latency_ms,omitempty"`
	Score      int    `json:"score"`
	Notes      string `json:"notes,omitempty"`
}

type Event struct {
	Index        int64     `json:"index"`
	RecordedAt   time.Time `json:"recorded_at"`
	ProviderID   string    `json:"provider_id"`
	NodeID       string    `json:"node_id,omitempty"`
	Challenge    string    `json:"challenge"`
	Passed       bool      `json:"passed"`
	LatencyMs    int64     `json:"latency_ms,omitempty"`
	Score        int       `json:"score"`
	Notes        string    `json:"notes,omitempty"`
	PreviousHash string    `json:"previous_hash"`
	Hash         string    `json:"hash"`
}

type ProviderSummary struct {
	ProviderID       string    `json:"provider_id"`
	Challenges       int64     `json:"challenges"`
	Passed           int64     `json:"passed"`
	Failed           int64     `json:"failed"`
	PassRateBPS      int64     `json:"pass_rate_bps"`
	AverageScore     int       `json:"average_score"`
	AverageLatencyMs int64     `json:"average_latency_ms,omitempty"`
	LastChallengeAt  time.Time `json:"last_challenge_at,omitempty"`
}

type Snapshot struct {
	Enabled      bool              `json:"enabled"`
	Path         string            `json:"path,omitempty"`
	Events       int               `json:"events"`
	LastHash     string            `json:"last_hash,omitempty"`
	Summaries    []ProviderSummary `json:"summaries,omitempty"`
	RecentEvents []Event           `json:"recent_events,omitempty"`
}

type Verification struct {
	Valid    bool   `json:"valid"`
	Events   int    `json:"events"`
	LastHash string `json:"last_hash,omitempty"`
	Error    string `json:"error,omitempty"`
}

func New(cfg config.ChallengeConfig) (*Ledger, error) {
	l := &Ledger{enabled: cfg.Enabled, path: cfg.Path, lastHash: genesisHash}
	if l.path != "" {
		if err := l.load(); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (l *Ledger) Record(input RecordInput) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.enabled {
		return Event{}, ErrDisabled
	}
	score := input.Score
	if score < 0 {
		score = 0
	}
	if score > 100 {
		score = 100
	}
	event := Event{
		Index:        int64(len(l.events) + 1),
		RecordedAt:   time.Now().UTC(),
		ProviderID:   input.ProviderID,
		NodeID:       input.NodeID,
		Challenge:    input.Challenge,
		Passed:       input.Passed,
		LatencyMs:    input.LatencyMs,
		Score:        score,
		Notes:        input.Notes,
		PreviousHash: l.lastHash,
	}
	event.Hash = hashEvent(event)
	if l.path != "" {
		if err := appendEvent(l.path, event); err != nil {
			return Event{}, err
		}
	}
	l.events = append(l.events, event)
	l.lastHash = event.Hash
	return event, nil
}

func (l *Ledger) Snapshot(limit int) Snapshot {
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 {
		limit = 20
	}
	start := len(l.events) - limit
	if start < 0 {
		start = 0
	}
	return Snapshot{
		Enabled:      l.enabled,
		Path:         l.path,
		Events:       len(l.events),
		LastHash:     l.lastHash,
		Summaries:    summaries(l.events),
		RecentEvents: append([]Event(nil), l.events[start:]...),
	}
}

func (l *Ledger) Verify() Verification {
	l.mu.Lock()
	defer l.mu.Unlock()
	last := genesisHash
	for i, event := range l.events {
		if event.Index != int64(i+1) || event.PreviousHash != last || event.Hash != hashEvent(event) {
			return Verification{Valid: false, Events: len(l.events), LastHash: last, Error: ErrInvalidChain.Error()}
		}
		last = event.Hash
	}
	return Verification{Valid: true, Events: len(l.events), LastHash: last}
}

func (l *Ledger) load() error {
	file, err := os.Open(l.path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	buf := make([]byte, 0, 1024*1024)
	scanner.Buffer(buf, 16*1024*1024)
	for scanner.Scan() {
		var event Event
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			return err
		}
		l.events = append(l.events, event)
		l.lastHash = event.Hash
	}
	if err := scanner.Err(); err != nil {
		return err
	}
	if verification := l.Verify(); !verification.Valid {
		return ErrInvalidChain
	}
	return nil
}

func appendEvent(path string, event Event) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return file.Sync()
}

func summaries(events []Event) []ProviderSummary {
	items := map[string]*ProviderSummary{}
	for _, event := range events {
		providerID := event.ProviderID
		if providerID == "" {
			providerID = "unknown"
		}
		item := items[providerID]
		if item == nil {
			item = &ProviderSummary{ProviderID: providerID}
			items[providerID] = item
		}
		item.Challenges++
		if event.Passed {
			item.Passed++
		} else {
			item.Failed++
		}
		item.AverageScore = int((int64(item.AverageScore)*(item.Challenges-1) + int64(event.Score)) / item.Challenges)
		item.AverageLatencyMs = (item.AverageLatencyMs*(item.Challenges-1) + event.LatencyMs) / item.Challenges
		if event.RecordedAt.After(item.LastChallengeAt) {
			item.LastChallengeAt = event.RecordedAt
		}
	}
	out := make([]ProviderSummary, 0, len(items))
	for _, item := range items {
		if item.Challenges > 0 {
			item.PassRateBPS = item.Passed * 10000 / item.Challenges
		}
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ProviderID < out[j].ProviderID })
	return out
}

func hashEvent(event Event) string {
	copyEvent := event
	copyEvent.Hash = ""
	data, _ := json.Marshal(copyEvent)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
