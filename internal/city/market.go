package city

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/protocol"
)

var (
	ErrUnauthorizedConsumer = errors.New("unauthorized consumer")
	ErrUnauthorizedProvider = errors.New("unauthorized provider")
	ErrQuotaExceeded        = errors.New("consumer token quota exceeded")
	ErrInvalidAccount       = errors.New("invalid account id")
	ErrAccountExists        = errors.New("account already exists")
)

type Market struct {
	enabled               bool
	name                  string
	requireProviderTokens bool
	usageStorePath        string

	mu           sync.Mutex
	consumers    map[string]Consumer
	providers    map[string]Provider
	apiKeys      map[string]string
	tokens       map[string]string
	apiKeyHashes map[string]string
	tokenHashes  map[string]string
	consumerUse  map[string]*Usage
	providerUse  map[string]*Usage
}

type persistedState struct {
	Version       int                 `json:"version"`
	UpdatedAt     time.Time           `json:"updated_at"`
	Consumers     []persistedConsumer `json:"consumers"`
	Providers     []persistedProvider `json:"providers"`
	ConsumerUsage []Usage             `json:"consumer_usage"`
	ProviderUsage []Usage             `json:"provider_usage"`
}

type persistedConsumer struct {
	ID              string   `json:"id"`
	DisplayName     string   `json:"display_name"`
	TotalTokenLimit int64    `json:"total_token_limit,omitempty"`
	APIKeyHashes    []string `json:"api_key_hashes,omitempty"`
}

type persistedProvider struct {
	ID          string   `json:"id"`
	DisplayName string   `json:"display_name"`
	TokenHashes []string `json:"token_hashes,omitempty"`
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

type CreateConsumerInput struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	TotalTokenLimit int64  `json:"total_token_limit,omitempty"`
}

type CreatedConsumer struct {
	Consumer Consumer `json:"consumer"`
	APIKey   string   `json:"api_key"`
}

type CreateProviderInput struct {
	ID          string `json:"id"`
	DisplayName string `json:"display_name"`
}

type CreatedProvider struct {
	Provider      Provider `json:"provider"`
	ProviderToken string   `json:"provider_token"`
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
		apiKeyHashes:          map[string]string{},
		tokenHashes:           map[string]string{},
		consumerUse:           map[string]*Usage{},
		providerUse:           map[string]*Usage{},
	}
	if cfg.UsageStorePath != "" {
		if err := m.loadState(cfg.UsageStorePath); err != nil {
			return nil, err
		}
	}
	for _, key := range legacyAPIKeys {
		if key != "" {
			m.apiKeys[key] = "local"
			m.apiKeyHashes[hashSecret(key)] = "local"
		}
	}
	if len(legacyAPIKeys) > 0 {
		m.consumers["local"] = Consumer{ID: "local", DisplayName: "Local"}
	}
	for _, consumer := range cfg.Consumers {
		if consumer.ID == "" {
			continue
		}
		m.consumers[consumer.ID] = Consumer{
			ID:              consumer.ID,
			DisplayName:     consumer.DisplayName,
			TotalTokenLimit: consumer.TotalTokenLimit,
		}
		for _, key := range consumer.APIKeys {
			if key != "" {
				m.apiKeys[key] = consumer.ID
				m.apiKeyHashes[hashSecret(key)] = consumer.ID
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
			m.tokenHashes[hashSecret(provider.Token)] = provider.ID
		}
	}
	return m, nil
}

func (m *Market) Enabled() bool {
	return m.enabled
}

func (m *Market) CreateConsumer(input CreateConsumerInput) (CreatedConsumer, error) {
	id := normalizeAccountID(input.ID)
	if id == "" {
		return CreatedConsumer{}, ErrInvalidAccount
	}
	apiKey, err := randomSecret("sk-mi-")
	if err != nil {
		return CreatedConsumer{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.consumers[id]; exists {
		return CreatedConsumer{}, ErrAccountExists
	}
	consumer := Consumer{ID: id, DisplayName: displayName(input.DisplayName, id), TotalTokenLimit: input.TotalTokenLimit}
	m.consumers[id] = consumer
	m.apiKeyHashes[hashSecret(apiKey)] = id
	if err := m.persistLocked(); err != nil {
		delete(m.consumers, id)
		delete(m.apiKeyHashes, hashSecret(apiKey))
		return CreatedConsumer{}, err
	}
	return CreatedConsumer{Consumer: consumer, APIKey: apiKey}, nil
}

func (m *Market) CreateProvider(input CreateProviderInput) (CreatedProvider, error) {
	id := normalizeAccountID(input.ID)
	if id == "" {
		return CreatedProvider{}, ErrInvalidAccount
	}
	token, err := randomSecret("pk-mi-")
	if err != nil {
		return CreatedProvider{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	if _, exists := m.providers[id]; exists {
		return CreatedProvider{}, ErrAccountExists
	}
	provider := Provider{ID: id, DisplayName: displayName(input.DisplayName, id)}
	m.providers[id] = provider
	m.tokenHashes[hashSecret(token)] = id
	if err := m.persistLocked(); err != nil {
		delete(m.providers, id)
		delete(m.tokenHashes, hashSecret(token))
		return CreatedProvider{}, err
	}
	return CreatedProvider{Provider: provider, ProviderToken: token}, nil
}

func (m *Market) AuthenticateConsumer(key string) (string, error) {
	if key == "" && !m.enabled && len(m.apiKeys) == 0 && len(m.apiKeyHashes) == 0 {
		return "local", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if accountID, ok := m.apiKeys[key]; ok {
		return accountID, nil
	}
	if accountID, ok := m.apiKeyHashes[hashSecret(key)]; ok {
		return accountID, nil
	}
	return "", ErrUnauthorizedConsumer
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
	if providerID, ok := m.tokens[reg.ProviderToken]; ok && providerID != "" {
		return providerID, nil
	}
	if providerID, ok := m.tokenHashes[hashSecret(reg.ProviderToken)]; ok && providerID != "" {
		return providerID, nil
	}
	return "", ErrUnauthorizedProvider
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

func (m *Market) loadState(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	var snapshot persistedState
	if err := json.Unmarshal(data, &snapshot); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, consumer := range snapshot.Consumers {
		m.consumers[consumer.ID] = Consumer{
			ID:              consumer.ID,
			DisplayName:     consumer.DisplayName,
			TotalTokenLimit: consumer.TotalTokenLimit,
		}
		for _, hash := range consumer.APIKeyHashes {
			m.apiKeyHashes[hash] = consumer.ID
		}
	}
	for _, provider := range snapshot.Providers {
		m.providers[provider.ID] = Provider{ID: provider.ID, DisplayName: provider.DisplayName}
		for _, hash := range provider.TokenHashes {
			m.tokenHashes[hash] = provider.ID
		}
	}
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
	snapshot := persistedState{
		Version:       2,
		UpdatedAt:     time.Now(),
		Consumers:     m.persistedConsumersLocked(),
		Providers:     m.persistedProvidersLocked(),
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

func (m *Market) persistedConsumersLocked() []persistedConsumer {
	consumers := make([]persistedConsumer, 0, len(m.consumers))
	for _, consumer := range consumerMapValues(m.consumers) {
		consumers = append(consumers, persistedConsumer{
			ID:              consumer.ID,
			DisplayName:     consumer.DisplayName,
			TotalTokenLimit: consumer.TotalTokenLimit,
			APIKeyHashes:    hashesForAccount(m.apiKeyHashes, consumer.ID),
		})
	}
	return consumers
}

func (m *Market) persistedProvidersLocked() []persistedProvider {
	providers := make([]persistedProvider, 0, len(m.providers))
	for _, provider := range providerMapValues(m.providers) {
		providers = append(providers, persistedProvider{
			ID:          provider.ID,
			DisplayName: provider.DisplayName,
			TokenHashes: hashesForAccount(m.tokenHashes, provider.ID),
		})
	}
	return providers
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

func usageMapValues(items map[string]*Usage) []Usage {
	values := make([]Usage, 0, len(items))
	for _, item := range items {
		values = append(values, *item)
	}
	sort.Slice(values, func(i, j int) bool { return values[i].AccountID < values[j].AccountID })
	return values
}

func hashesForAccount(items map[string]string, accountID string) []string {
	hashes := []string{}
	for hash, id := range items {
		if id == accountID {
			hashes = append(hashes, hash)
		}
	}
	sort.Strings(hashes)
	return hashes
}

func hashSecret(secret string) string {
	sum := sha256.Sum256([]byte(secret))
	return hex.EncodeToString(sum[:])
}

func randomSecret(prefix string) (string, error) {
	var raw [24]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(raw[:]), nil
}

func normalizeAccountID(raw string) string {
	id := strings.ToLower(strings.TrimSpace(raw))
	if len(id) < 2 || len(id) > 64 {
		return ""
	}
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	return id
}

func displayName(raw, fallback string) string {
	name := strings.TrimSpace(raw)
	if name == "" {
		return fallback
	}
	return name
}
