package settlement

import (
	"bufio"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/protocol"
	"github.com/raym33/mi/internal/sqlitestore"
)

const genesisHash = "genesis"

var ErrInvalidChain = errors.New("invalid settlement chain")

type Ledger struct {
	mu                sync.Mutex
	enabled           bool
	path              string
	sqlitePath        string
	db                *sql.DB
	price             int64
	shareBPS          int64
	targetLatencyMs   int64
	latencyPenaltyBPS int64
	events            []Event
	lastHash          string
}

type RecordInput struct {
	RequestID        string
	ConsumerID       string
	ProviderID       string
	NodeID           string
	Model            string
	PrivacyTier      string
	Done             protocol.InferDone
	Latency          time.Duration
	DispatchAttempts int
}

type Event struct {
	Index                 int64     `json:"index"`
	RecordedAt            time.Time `json:"recorded_at"`
	RequestID             string    `json:"request_id"`
	ConsumerID            string    `json:"consumer_id"`
	ProviderID            string    `json:"provider_id,omitempty"`
	NodeID                string    `json:"node_id,omitempty"`
	Model                 string    `json:"model"`
	PrivacyTier           string    `json:"privacy_tier"`
	PromptTokens          int       `json:"prompt_tokens"`
	CompletionTokens      int       `json:"completion_tokens"`
	TotalTokens           int       `json:"total_tokens"`
	LatencyMs             int64     `json:"latency_ms,omitempty"`
	DispatchAttempts      int       `json:"dispatch_attempts,omitempty"`
	PriceMicros           int64     `json:"price_micros"`
	ProviderRewardMicros  int64     `json:"provider_reward_micros"`
	ProviderPenaltyMicros int64     `json:"provider_penalty_micros,omitempty"`
	PreviousHash          string    `json:"previous_hash"`
	Hash                  string    `json:"hash"`
}

type Balance struct {
	AccountID        string `json:"account_id"`
	Events           int64  `json:"events"`
	TotalTokens      int64  `json:"total_tokens"`
	AverageLatencyMs int64  `json:"average_latency_ms,omitempty"`
	DebitMicros      int64  `json:"debit_micros,omitempty"`
	RewardMicros     int64  `json:"reward_micros,omitempty"`
	PenaltyMicros    int64  `json:"penalty_micros,omitempty"`
}

type Snapshot struct {
	Enabled                      bool      `json:"enabled"`
	ChainPath                    string    `json:"chain_path,omitempty"`
	SQLitePath                   string    `json:"sqlite_path,omitempty"`
	Events                       int       `json:"events"`
	LastHash                     string    `json:"last_hash,omitempty"`
	PricePerThousandTokensMicros int64     `json:"price_per_thousand_tokens_micros,omitempty"`
	ProviderRewardShareBPS       int64     `json:"provider_reward_share_bps,omitempty"`
	TargetLatencyMs              int64     `json:"target_latency_ms,omitempty"`
	LatencyPenaltyBPS            int64     `json:"latency_penalty_bps,omitempty"`
	ConsumerBalances             []Balance `json:"consumer_balances,omitempty"`
	ProviderBalances             []Balance `json:"provider_balances,omitempty"`
	RecentEvents                 []Event   `json:"recent_events,omitempty"`
}

type Verification struct {
	Valid    bool   `json:"valid"`
	Events   int    `json:"events"`
	LastHash string `json:"last_hash,omitempty"`
	Error    string `json:"error,omitempty"`
}

func New(cfg config.SettlementConfig) (*Ledger, error) {
	price := cfg.PricePerThousandTokensMicros
	if price < 0 {
		price = 0
	}
	share := cfg.ProviderRewardShareBPS
	if share < 0 {
		share = 0
	}
	if share > 10000 {
		share = 10000
	}
	penalty := cfg.LatencyPenaltyBPS
	if penalty < 0 {
		penalty = 0
	}
	if penalty > 10000 {
		penalty = 10000
	}
	l := &Ledger{
		enabled:           cfg.Enabled,
		path:              cfg.ChainPath,
		sqlitePath:        cfg.SQLitePath,
		price:             price,
		shareBPS:          share,
		targetLatencyMs:   cfg.TargetLatencyMs,
		latencyPenaltyBPS: penalty,
		lastHash:          genesisHash,
	}
	if l.sqlitePath != "" {
		db, err := sqlitestore.Open(l.sqlitePath)
		if err != nil {
			return nil, err
		}
		l.db = db
		if err := l.initSQLite(); err != nil {
			_ = db.Close()
			return nil, err
		}
		if err := l.loadSQLite(); err != nil {
			_ = db.Close()
			return nil, err
		}
	} else if l.path != "" {
		if err := l.load(); err != nil {
			return nil, err
		}
	}
	return l, nil
}

func (l *Ledger) Close() error {
	if l.db == nil {
		return nil
	}
	return l.db.Close()
}

func (l *Ledger) Record(input RecordInput) (Event, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.enabled {
		return Event{}, nil
	}
	totalTokens := input.Done.PromptTokens + input.Done.OutputTokens
	priceMicros := l.priceFor(totalTokens)
	latencyMs := input.Latency.Milliseconds()
	rewardMicros := priceMicros * l.shareBPS / 10000
	penaltyMicros := l.penaltyFor(rewardMicros, latencyMs)
	event := Event{
		Index:                 int64(len(l.events) + 1),
		RecordedAt:            time.Now().UTC(),
		RequestID:             input.RequestID,
		ConsumerID:            input.ConsumerID,
		ProviderID:            input.ProviderID,
		NodeID:                input.NodeID,
		Model:                 input.Model,
		PrivacyTier:           input.PrivacyTier,
		PromptTokens:          input.Done.PromptTokens,
		CompletionTokens:      input.Done.OutputTokens,
		TotalTokens:           totalTokens,
		LatencyMs:             latencyMs,
		DispatchAttempts:      input.DispatchAttempts,
		PriceMicros:           priceMicros,
		ProviderRewardMicros:  rewardMicros - penaltyMicros,
		ProviderPenaltyMicros: penaltyMicros,
		PreviousHash:          l.lastHash,
	}
	event.Hash = hashEvent(event)
	if l.db != nil {
		if err := l.insertSQLiteEvent(event); err != nil {
			return Event{}, err
		}
	} else if l.path != "" {
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
	consumerBalances, providerBalances := balances(l.events)
	start := len(l.events) - limit
	if start < 0 {
		start = 0
	}
	recent := append([]Event(nil), l.events[start:]...)
	return Snapshot{
		Enabled:                      l.enabled,
		ChainPath:                    l.path,
		SQLitePath:                   l.sqlitePath,
		Events:                       len(l.events),
		LastHash:                     l.lastHash,
		PricePerThousandTokensMicros: l.price,
		ProviderRewardShareBPS:       l.shareBPS,
		TargetLatencyMs:              l.targetLatencyMs,
		LatencyPenaltyBPS:            l.latencyPenaltyBPS,
		ConsumerBalances:             consumerBalances,
		ProviderBalances:             providerBalances,
		RecentEvents:                 recent,
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

func (l *Ledger) priceFor(tokens int) int64 {
	if tokens <= 0 || l.price <= 0 {
		return 0
	}
	return int64(tokens) * l.price / 1000
}

func (l *Ledger) penaltyFor(rewardMicros int64, latencyMs int64) int64 {
	if rewardMicros <= 0 || l.targetLatencyMs <= 0 || l.latencyPenaltyBPS <= 0 {
		return 0
	}
	if latencyMs <= l.targetLatencyMs {
		return 0
	}
	return rewardMicros * l.latencyPenaltyBPS / 10000
}

func (l *Ledger) initSQLite() error {
	_, err := l.db.Exec(`
CREATE TABLE IF NOT EXISTS settlement_events (
	event_index INTEGER PRIMARY KEY,
	recorded_at TEXT NOT NULL,
	request_id TEXT NOT NULL,
	consumer_id TEXT NOT NULL,
	provider_id TEXT NOT NULL,
	node_id TEXT NOT NULL,
	model TEXT NOT NULL,
	privacy_tier TEXT NOT NULL,
	prompt_tokens INTEGER NOT NULL,
	completion_tokens INTEGER NOT NULL,
	total_tokens INTEGER NOT NULL,
	latency_ms INTEGER NOT NULL,
	dispatch_attempts INTEGER NOT NULL,
	price_micros INTEGER NOT NULL,
	provider_reward_micros INTEGER NOT NULL,
	provider_penalty_micros INTEGER NOT NULL,
	previous_hash TEXT NOT NULL,
	hash TEXT NOT NULL
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_settlement_hash ON settlement_events(hash);
CREATE INDEX IF NOT EXISTS idx_settlement_consumer ON settlement_events(consumer_id);
CREATE INDEX IF NOT EXISTS idx_settlement_provider ON settlement_events(provider_id);
`)
	return err
}

func (l *Ledger) loadSQLite() error {
	rows, err := l.db.Query(`
SELECT event_index, recorded_at, request_id, consumer_id, provider_id, node_id, model, privacy_tier,
       prompt_tokens, completion_tokens, total_tokens, latency_ms, dispatch_attempts,
       price_micros, provider_reward_micros, provider_penalty_micros, previous_hash, hash
FROM settlement_events
ORDER BY event_index ASC`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var event Event
		var recordedAt string
		if err := rows.Scan(
			&event.Index,
			&recordedAt,
			&event.RequestID,
			&event.ConsumerID,
			&event.ProviderID,
			&event.NodeID,
			&event.Model,
			&event.PrivacyTier,
			&event.PromptTokens,
			&event.CompletionTokens,
			&event.TotalTokens,
			&event.LatencyMs,
			&event.DispatchAttempts,
			&event.PriceMicros,
			&event.ProviderRewardMicros,
			&event.ProviderPenaltyMicros,
			&event.PreviousHash,
			&event.Hash,
		); err != nil {
			return err
		}
		parsed, err := time.Parse(time.RFC3339Nano, recordedAt)
		if err != nil {
			return err
		}
		event.RecordedAt = parsed
		l.events = append(l.events, event)
		l.lastHash = event.Hash
	}
	if err := rows.Err(); err != nil {
		return err
	}
	verification := l.Verify()
	if !verification.Valid {
		return ErrInvalidChain
	}
	return nil
}

func (l *Ledger) insertSQLiteEvent(event Event) error {
	_, err := l.db.Exec(`
INSERT INTO settlement_events (
	event_index, recorded_at, request_id, consumer_id, provider_id, node_id, model, privacy_tier,
	prompt_tokens, completion_tokens, total_tokens, latency_ms, dispatch_attempts,
	price_micros, provider_reward_micros, provider_penalty_micros, previous_hash, hash
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		event.Index,
		event.RecordedAt.Format(time.RFC3339Nano),
		event.RequestID,
		event.ConsumerID,
		event.ProviderID,
		event.NodeID,
		event.Model,
		event.PrivacyTier,
		event.PromptTokens,
		event.CompletionTokens,
		event.TotalTokens,
		event.LatencyMs,
		event.DispatchAttempts,
		event.PriceMicros,
		event.ProviderRewardMicros,
		event.ProviderPenaltyMicros,
		event.PreviousHash,
		event.Hash,
	)
	return err
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
	verification := l.Verify()
	if !verification.Valid {
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

func hashEvent(event Event) string {
	copyEvent := event
	copyEvent.Hash = ""
	data, _ := json.Marshal(copyEvent)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func balances(events []Event) ([]Balance, []Balance) {
	consumers := map[string]*Balance{}
	providers := map[string]*Balance{}
	for _, event := range events {
		if event.ConsumerID != "" {
			balance := consumers[event.ConsumerID]
			if balance == nil {
				balance = &Balance{AccountID: event.ConsumerID}
				consumers[event.ConsumerID] = balance
			}
			balance.Events++
			balance.TotalTokens += int64(event.TotalTokens)
			balance.AverageLatencyMs = ((balance.AverageLatencyMs * (balance.Events - 1)) + event.LatencyMs) / balance.Events
			balance.DebitMicros += event.PriceMicros
		}
		if event.ProviderID != "" {
			balance := providers[event.ProviderID]
			if balance == nil {
				balance = &Balance{AccountID: event.ProviderID}
				providers[event.ProviderID] = balance
			}
			balance.Events++
			balance.TotalTokens += int64(event.TotalTokens)
			balance.AverageLatencyMs = ((balance.AverageLatencyMs * (balance.Events - 1)) + event.LatencyMs) / balance.Events
			balance.RewardMicros += event.ProviderRewardMicros
			balance.PenaltyMicros += event.ProviderPenaltyMicros
		}
	}
	return balanceValues(consumers), balanceValues(providers)
}

func balanceValues(items map[string]*Balance) []Balance {
	values := make([]Balance, 0, len(items))
	for _, item := range items {
		values = append(values, *item)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].AccountID < values[j].AccountID })
	return values
}
