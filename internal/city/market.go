package city

import (
	"errors"
	"sync"
	"time"

	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/protocol"
)

var (
	ErrUnauthorizedConsumer = errors.New("unauthorized consumer")
	ErrUnauthorizedProvider = errors.New("unauthorized provider")
)

type Market struct {
	enabled               bool
	name                  string
	requireProviderTokens bool

	mu          sync.Mutex
	consumers   map[string]Consumer
	providers   map[string]Provider
	apiKeys     map[string]string
	tokens      map[string]string
	consumerUse map[string]*Usage
	providerUse map[string]*Usage
}

type Consumer struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
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

func New(cfg config.CityConfig, legacyAPIKeys []string) *Market {
	m := &Market{
		enabled:               cfg.Enabled,
		name:                  cfg.Name,
		requireProviderTokens: cfg.RequireProviderTokens,
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
		m.consumers[consumer.ID] = Consumer{ID: consumer.ID, DisplayName: consumer.DisplayName}
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
	return m
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

func (m *Market) Record(consumerID, providerID string, done protocol.InferDone) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addUsage(m.consumerUse, consumerID, done)
	if providerID != "" {
		m.addUsage(m.providerUse, providerID, done)
	}
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

func (m *Market) Snapshot() Snapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	s := Snapshot{Enabled: m.enabled, Name: m.name}
	for _, consumer := range m.consumers {
		s.Consumers = append(s.Consumers, consumer)
	}
	for _, provider := range m.providers {
		s.Providers = append(s.Providers, provider)
	}
	for _, usage := range m.consumerUse {
		s.ConsumerUsage = append(s.ConsumerUsage, *usage)
	}
	for _, usage := range m.providerUse {
		s.ProviderUsage = append(s.ProviderUsage, *usage)
	}
	return s
}
