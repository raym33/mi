package city

import (
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

var (
	ErrUnauthorizedConsumer = errors.New("unauthorized consumer")
	ErrUnauthorizedProvider = errors.New("unauthorized provider")
	ErrQuotaExceeded        = errors.New("consumer token quota exceeded")
)

type Market struct {
	enabled               bool
	name                  string
	requireProviderTokens bool
	usageStorePath        string

	mu          sync.Mutex
	consumers   map[string]Consumer
	providers   map[string]Provider
	apiKeys     map[string]string
	tokens      map[string]string
	consumerUse map[string]*Usage
	providerUse map[string]*Usage
}

type persistedUsage struct {
	Version       int       `json:"version"`
	UpdatedAt     time.Time `json:"updated_at"`
	ConsumerUsage []Usage   `json:"consumer_usage"`
	ProviderUsage []Usage   `json:"provider_usage"`
}

type Consumer struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	TotalTokenLimit int64  `json:"total_token_limit,omitempty"`
}

type Provider struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type Usage struct {
	AccountID        string    `json:"account_id"`
	Requests         int64     `json:"requests"`
	PromptTokens     int64     `json:"prompt_tokens"`
	CompletionTokens int64     `json:"completion_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	UpdatedAt        time.Time `json:"updated_at"`
}

type Snapshot struct {
	Enabled       bool       `json:"enabled"`
	Name          string     `json:"name"`
	Consumers     []Consumer `json:"consumers"`
	Providers     []Provider `json:"providers"`
	ConsumerUsage []Usage    `json:"consumer_usage"`
	ProviderUsage []Usage    `json:"provider_usage"`
}

type ConsumerStatus struct {
	Consumer        Consumer `json:"consumer"`
	Usage           Usage    `json:"usage"`
	RemainingTokens int64    `json:"remaining_tokens,omitempty"`
	QuotaExceeded   bool     `json:"quota_exceeded"`
}

func New(cfg config.CityConfig, legacyAPIKeys []string) (*Market, error) {
	m := &Market{
		enabled:               cfg.Enabled,
		name:                  cfg.Name,
		requireProviderTokens: cfg.RequireProviderTokens,
		usageStorePath:        cfg.UsageStorePath,
		consumers:             map[string]Consumer{},
		providers:             map[string]Provider{},
		apiKeys:               map[string]string{},
		tokens:                map[string]string{},
		consumerUse:           map[string]*Usage{},
		providerUse:           map[string]*Usage{},
	}
	for _, key := range legacyAPIKeys {
		if key != "" {
			m.apiKeys[key] = "local"
		}
	}
	if len(legacyAPIKeys) > 0 {
		m.consumers["local"] = Consumer{ID: "local", DisplayName: "Local"}
	}
	for _, consumer := range cfg.Consumers {
		if consumer.ID == "" {
			continue
		}
		m.consumers[consumer.ID] = Consumer{ID: consumer.ID, DisplayName: consumer.DisplayName, TotalTokenLimit: consumer.TotalTokenLimit}
		for _, key := range consumer.APIKeys {
			if key != "" {
				m.apiKeys[key] = consumer.ID
			}
		}
	}
	for _, provider := range cfg.Providers {
		if provider.ID == "" {
			continue
		}
		m.providers[provider.ID] = Provider{ID: provider.ID, DisplayName: provider.DisplayName}
		if provider.Token != "" {
			m.tokens[provider.Token] = provider.ID
		}
	}
	if cfg.UsageStorePath != "" {
		if err := m.loadUsage(cfg.UsageStorePath); err != nil {
			return nil, err
		}
	}
	return m, nil
}

func (m *Market) Enabled() bool {
	return m.enabled
}

func (m *Market) AuthenticateConsumer(key string) (string, error) {
	if key == "" && !m.enabled && len(m.apiKeys) == 0 {
		return "local", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	accountID, ok := m.apiKeys[key]
	if !ok {
		return "", ErrUnauthorizedConsumer
	}
	return accountID, nil
}

func (m *Market) CheckConsumerQuota(accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	consumer, ok := m.consumers[accountID]
	if !ok || consumer.TotalTokenLimit <= 0 {
		return nil
	}
	usage := m.consumerUse[accountID]
	if usage != nil && usage.TotalTokens >= consumer.TotalTokenLimit {
		return ErrQuotaExceeded
	}
	return nil
}

func (m *Market) AuthenticateProvider(reg protocol.Register) (string, error) {
	if !m.enabled || !m.requireProviderTokens {
		if reg.ProviderID != "" {
			return reg.ProviderID, nil
		}
		return reg.NodeID, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	providerID, ok := m.tokens[reg.ProviderToken]
	if !ok || providerID == "" {
		return "", ErrUnauthorizedProvider
	}
	return providerID, nil
}

func (m *Market) Record(consumerID, providerID string, done protocol.InferDone) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addUsage(m.consumerUse, consumerID, done)
	if providerID != "" {
		m.addUsage(m.providerUse, providerID, done)
	}
	return m.persistLocked()
}

func (m *Market) ConsumerStatus(accountID string) ConsumerStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	consumer := m.consumers[accountID]
	if consumer.ID == "" {
		consumer = Consumer{ID: accountID, DisplayName: accountID}
	}
	var usage Usage
	if current := m.consumerUse[accountID]; current != nil {
		usage = *current
	} else {
		usage = Usage{AccountID: accountID}
	}
	status := ConsumerStatus{Consumer: consumer, Usage: usage}
	if consumer.TotalTokenLimit > 0 {
		status.RemainingTokens = consumer.TotalTokenLimit - usage.TotalTokens
		if status.RemainingTokens < 0 {
			status.RemainingTokens = 0
		}
		status.QuotaExceeded = usage.TotalTokens >= consumer.TotalTokenLimit
	}
	return status
}

func (m *Market) addUsage(bucket map[string]*Usage, accountID string, done protocol.InferDone) {
	if accountID == "" {
		accountID = "unknown"
	}
	usage := bucket[accountID]
	if usage == nil {
		usage = &Usage{AccountID: accountID}
		bucket[accountID] = usage
	}
	usage.Requests++
	usage.PromptTokens += int64(done.PromptTokens)
	usage.CompletionTokens += int64(done.OutputTokens)
	usage.TotalTokens = usage.PromptTokens + usage.CompletionTokens
	usage.UpdatedAt = time.Now()
}

func (m *Market) loadUsage(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var snapshot persistedUsage
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, usage := range snapshot.ConsumerUsage {
		copyUsage := usage
		m.consumerUse[usage.AccountID] = &copyUsage
	}
	for _, usage := range snapshot.ProviderUsage {
		copyUsage := usage
		m.providerUse[usage.AccountID] = &copyUsage
	}
	return nil
}

func (m *Market) persistLocked() error {
	if m.usageStorePath == "" {
		return nil
	}
	snapshot := persistedUsage{
		Version:       1,
		UpdatedAt:     time.Now(),
		ConsumerUsage: usageMapValues(m.consumerUse),
		ProviderUsage: usageMapValues(m.providerUse),
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(m.usageStorePath), 0o755); err != nil {
		return err
	}
	tmp := m.usageStorePath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, m.usageStorePath)
}

func usageMapValues(items map[string]*Usage) []Usage {
	values := make([]Usage, 0, len(items))
	for _, item := range items {
		values = append(values, *item)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].AccountID < values[j].AccountID })
	return values
}

func (m *Market) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	return Snapshot{
		Enabled:       m.enabled,
		Name:          m.name,
		Consumers:     consumerMapValues(m.consumers),
		Providers:     providerMapValues(m.providers),
		ConsumerUsage: usageMapValues(m.consumerUse),
		ProviderUsage: usageMapValues(m.providerUse),
	}
}

func consumerMapValues(items map[string]Consumer) []Consumer {
	values := make([]Consumer, 0, len(items))
	for _, consumer := range items {
		values = append(values, consumer)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values
}

func providerMapValues(items map[string]Provider) []Provider {
	values := make([]Provider, 0, len(items))
	for _, provider := range items {
		values = append(values, provider)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].ID < values[j].ID })
	return values
}
