package settlement

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
	"github.com/raym33/mi/internal/protocol"
)

const genesisHash = "genesis"

var ErrInvalidChain = errors.New("invalid settlement chain")

type Ledger struct {
	mu       sync.Mutex
	enabled  bool
	path     string
	price    int64
	shareBPS int64
	events   []Event
	lastHash string
}

type RecordInput struct {
	RequestID   string
	ConsumerID  string
	ProviderID  string
	NodeID      string
	Model       string
	PrivacyTier string
	Done        protocol.InferDone
}

type Event struct {
	Index                int64     `json:"index"`
	RecordedAt           time.Time `json:"recorded_at"`
	RequestID            string    `json:"request_id"`
	ConsumerID           string    `json:"consumer_id"`
	ProviderID           string    `json:"provider_id,omitempty"`
	NodeID               string    `json:"node_id,omitempty"`
	Model                string    `json:"model"`
	PrivacyTier          string    `json:"privacy_tier"`
	PromptTokens         int       `json:"prompt_tokens"`
	CompletionTokens     int       `json:"completion_tokens"`
	TotalTokens          int       `json:"total_tokens"`
	PriceMicros          int64     `json:"price_micros"`
	ProviderRewardMicros int64     `json:"provider_reward_micros"`
	PreviousHash         string    `json:"previous_hash"`
	Hash                 string    `json:"hash"`
}

type Balance struct {
	AccountID    string `json:"account_id"`
	Events       int64  `json:"events"`
	TotalTokens  int64  `json:"total_tokens"`
	DebitMicros  int64  `json:"debit_micros,omitempty"`
	RewardMicros int64  `json:"reward_micros,omitempty"`
}

type Snapshot struct {
	Enabled                      bool      `json:"enabled"`
	ChainPath                    string    `json:"chain_path,omitempty"`
	Events                       int       `json:"events"`
	LastHash                     string    `json:"last_hash,omitempty"`
	PricePerThousandTokensMicros int64     `json:"price_per_thousand_tokens_micros,omitempty"`
	ProviderRewardShareBPS       int64     `json:"provider_reward_share_bps,omitempty"`
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
	l := &Ledger{
		enabled:  cfg.Enabled,
		path:     cfg.ChainPath,
		price:    price,
		shareBPS: share,
		lastHash: genesisHash,
	}
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
		return Event{}, nil
	}
	totalTokens := input.Done.PromptTokens + input.Done.OutputTokens
	priceMicros := l.priceFor(totalTokens)
	event := Event{
		Index:                int64(len(l.events) + 1),
		RecordedAt:           time.Now().UTC(),
		RequestID:            input.RequestID,
		ConsumerID:           input.ConsumerID,
		ProviderID:           input.ProviderID,
		NodeID:               input.NodeID,
		Model:                input.Model,
		PrivacyTier:          input.PrivacyTier,
		PromptTokens:         input.Done.PromptTokens,
		CompletionTokens:     input.Done.OutputTokens,
		TotalTokens:          totalTokens,
		PriceMicros:          priceMicros,
		ProviderRewardMicros: priceMicros * l.shareBPS / 10000,
		PreviousHash:         l.lastHash,
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
	consumerBalances, providerBalances := balances(l.events)
	start := len(l.events) - limit
	if start < 0 {
		start = 0
	}
	recent := append([]Event(nil), l.events[start:]...)
	return Snapshot{
		Enabled:                      l.enabled,
		ChainPath:                    l.path,
		Events:                       len(l.events),
		LastHash:                     l.lastHash,
		PricePerThousandTokensMicros: l.price,
		ProviderRewardShareBPS:       l.shareBPS,
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
			balance.RewardMicros += event.ProviderRewardMicros
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
