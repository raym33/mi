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
	"github.com/raym33/mi/internal/privacy"
	"github.com/raym33/mi/internal/protocol"
)

var (
	ErrUnauthorizedConsumer = errors.New("unauthorized consumer")
	ErrUnauthorizedProvider = errors.New("unauthorized provider")
	ErrQuotaExceeded        = errors.New("consumer token quota exceeded")
	ErrInvalidAccount       = errors.New("invalid account id")
	ErrAccountExists        = errors.New("account already exists")
	ErrAccountNotFound      = errors.New("account not found")
	ErrAccountDisabled      = errors.New("account disabled")
	ErrInvalidPrivacy       = errors.New("invalid privacy policy")
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
	reservedUse  map[string]int64
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
	Disabled        bool     `json:"disabled,omitempty"`
	APIKeyHashes    []string `json:"api_key_hashes,omitempty"`
}

type persistedProvider struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	Disabled     bool     `json:"disabled,omitempty"`
	PrivacyMode  string   `json:"privacy_mode,omitempty"`
	PrivacyTiers []string `json:"privacy_tiers,omitempty"`
	TokenHashes  []string `json:"token_hashes,omitempty"`
}

type Consumer struct {
	ID              string `json:"id"`
	DisplayName     string `json:"display_name"`
	TotalTokenLimit int64  `json:"total_token_limit,omitempty"`
	Disabled        bool   `json:"disabled,omitempty"`
}

type Provider struct {
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	Disabled     bool     `json:"disabled,omitempty"`
	PrivacyMode  string   `json:"privacy_mode,omitempty"`
	PrivacyTiers []string `json:"privacy_tiers,omitempty"`
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
	ReservedTokens  int64    `json:"reserved_tokens,omitempty"`
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
	ID           string   `json:"id"`
	DisplayName  string   `json:"display_name"`
	PrivacyMode  string   `json:"privacy_mode,omitempty"`
	PrivacyTiers []string `json:"privacy_tiers,omitempty"`
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
		reservedUse:           map[string]int64{},
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
		disabled := m.consumers[consumer.ID].Disabled
		m.consumers[consumer.ID] = Consumer{
			ID:              consumer.ID,
			DisplayName:     consumer.DisplayName,
			TotalTokenLimit: consumer.TotalTokenLimit,
			Disabled:        disabled,
		}
		for _, key := range consumer.APIKeys {
			if key != "" && !disabled {
				m.apiKeys[key] = consumer.ID
				m.apiKeyHashes[hashSecret(key)] = consumer.ID
			}
		}
	}
	for _, provider := range cfg.Providers {
		if provider.ID == "" {
			continue
		}
		disabled := m.providers[provider.ID].Disabled
		privacyMode, privacyTiers, err := providerPrivacy(provider.PrivacyMode, provider.PrivacyTiers)
		if err != nil {
			return nil, err
		}
		m.providers[provider.ID] = Provider{ID: provider.ID, DisplayName: provider.DisplayName, Disabled: disabled, PrivacyMode: privacyMode, PrivacyTiers: privacyTiers}
		if provider.Token != "" && !disabled {
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
	privacyMode, privacyTiers, err := providerPrivacy(input.PrivacyMode, input.PrivacyTiers)
	if err != nil {
		return CreatedProvider{}, ErrInvalidPrivacy
	}
	provider := Provider{ID: id, DisplayName: displayName(input.DisplayName, id), PrivacyMode: privacyMode, PrivacyTiers: privacyTiers}
	m.providers[id] = provider
	m.tokenHashes[hashSecret(token)] = id
	if err := m.persistLocked(); err != nil {
		delete(m.providers, id)
		delete(m.tokenHashes, hashSecret(token))
		return CreatedProvider{}, err
	}
	return CreatedProvider{Provider: provider, ProviderToken: token}, nil
}

func (m *Market) RotateConsumerKey(rawID string) (CreatedConsumer, error) {
	id := normalizeAccountID(rawID)
	if id == "" {
		return CreatedConsumer{}, ErrInvalidAccount
	}
	apiKey, err := randomSecret("sk-mi-")
	if err != nil {
		return CreatedConsumer{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	consumer, ok := m.consumers[id]
	if !ok {
		return CreatedConsumer{}, ErrAccountNotFound
	}
	if consumer.Disabled {
		return CreatedConsumer{}, ErrAccountDisabled
	}
	m.removeConsumerSecretsLocked(id)
	m.apiKeyHashes[hashSecret(apiKey)] = id
	if err := m.persistLocked(); err != nil {
		return CreatedConsumer{}, err
	}
	return CreatedConsumer{Consumer: consumer, APIKey: apiKey}, nil
}

func (m *Market) RotateProviderToken(rawID string) (CreatedProvider, error) {
	id := normalizeAccountID(rawID)
	if id == "" {
		return CreatedProvider{}, ErrInvalidAccount
	}
	token, err := randomSecret("pk-mi-")
	if err != nil {
		return CreatedProvider{}, err
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	provider, ok := m.providers[id]
	if !ok {
		return CreatedProvider{}, ErrAccountNotFound
	}
	if provider.Disabled {
		return CreatedProvider{}, ErrAccountDisabled
	}
	m.removeProviderSecretsLocked(id)
	m.tokenHashes[hashSecret(token)] = id
	if err := m.persistLocked(); err != nil {
		return CreatedProvider{}, err
	}
	return CreatedProvider{Provider: provider, ProviderToken: token}, nil
}

func (m *Market) DisableConsumer(rawID string) (Consumer, error) {
	id := normalizeAccountID(rawID)
	if id == "" {
		return Consumer{}, ErrInvalidAccount
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	consumer, ok := m.consumers[id]
	if !ok {
		return Consumer{}, ErrAccountNotFound
	}
	consumer.Disabled = true
	m.consumers[id] = consumer
	m.removeConsumerSecretsLocked(id)
	if err := m.persistLocked(); err != nil {
		return Consumer{}, err
	}
	return consumer, nil
}

func (m *Market) DisableProvider(rawID string) (Provider, error) {
	id := normalizeAccountID(rawID)
	if id == "" {
		return Provider{}, ErrInvalidAccount
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	provider, ok := m.providers[id]
	if !ok {
		return Provider{}, ErrAccountNotFound
	}
	provider.Disabled = true
	m.providers[id] = provider
	m.removeProviderSecretsLocked(id)
	if err := m.persistLocked(); err != nil {
		return Provider{}, err
	}
	return provider, nil
}

func (m *Market) AuthenticateConsumer(key string) (string, error) {
	if key == "" && !m.enabled && len(m.apiKeys) == 0 && len(m.apiKeyHashes) == 0 {
		return "local", nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if accountID, ok := m.apiKeys[key]; ok {
		return m.activeConsumerIDLocked(accountID)
	}
	if accountID, ok := m.apiKeyHashes[hashSecret(key)]; ok {
		return m.activeConsumerIDLocked(accountID)
	}
	return "", ErrUnauthorizedConsumer
}

func (m *Market) CheckConsumerQuota(accountID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.checkConsumerQuotaLocked(accountID, 0)
}

type QuotaReservation struct {
	accountID string
	tokens    int64
}

func (m *Market) ReserveConsumerQuota(accountID string, estimatedTokens int64) (*QuotaReservation, error) {
	if estimatedTokens < 0 {
		estimatedTokens = 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.checkConsumerQuotaLocked(accountID, estimatedTokens); err != nil {
		return nil, err
	}
	if estimatedTokens == 0 {
		return nil, nil
	}
	consumer, ok := m.consumers[accountID]
	if !ok || consumer.TotalTokenLimit <= 0 {
		return nil, nil
	}
	m.reservedUse[accountID] += estimatedTokens
	return &QuotaReservation{accountID: accountID, tokens: estimatedTokens}, nil
}

func (m *Market) ReleaseReservation(reservation *QuotaReservation) {
	if reservation == nil || reservation.tokens <= 0 {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseReservationLocked(reservation)
}

func (m *Market) RecordReserved(reservation *QuotaReservation, consumerID, providerID string, done protocol.InferDone) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.releaseReservationLocked(reservation)
	m.addUsage(m.consumerUse, consumerID, done)
	if providerID != "" {
		m.addUsage(m.providerUse, providerID, done)
	}
	return m.persistLocked()
}

func (m *Market) checkConsumerQuotaLocked(accountID string, additionalTokens int64) error {
	consumer, ok := m.consumers[accountID]
	if !ok || consumer.TotalTokenLimit <= 0 {
		return nil
	}
	usage := m.consumerUse[accountID]
	used := m.reservedUse[accountID] + additionalTokens
	if usage != nil {
		used += usage.TotalTokens
	}
	if additionalTokens == 0 && used >= consumer.TotalTokenLimit {
		return ErrQuotaExceeded
	}
	if additionalTokens > 0 && used > consumer.TotalTokenLimit {
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
		return m.activeProviderIDLocked(providerID)
	}
	if providerID, ok := m.tokenHashes[hashSecret(reg.ProviderToken)]; ok && providerID != "" {
		return m.activeProviderIDLocked(providerID)
	}
	return "", ErrUnauthorizedProvider
}

func (m *Market) EnforceProviderPrivacy(providerID string, requestedMode string, requestedTiers []string) (string, []string, error) {
	requested, err := requestedPrivacy(requestedMode, requestedTiers)
	if err != nil {
		return "", nil, ErrInvalidPrivacy
	}
	if !m.enabled || !m.requireProviderTokens {
		return privacy.ModeForTiers(requested), requested, nil
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	provider, ok := m.providers[providerID]
	if !ok || provider.Disabled {
		return "", nil, ErrUnauthorizedProvider
	}
	allowed := provider.PrivacyTiers
	if len(allowed) == 0 {
		allowed, err = privacy.TiersForMode(provider.PrivacyMode)
		if err != nil {
			return "", nil, ErrInvalidPrivacy
		}
	}
	allowedSet := map[string]bool{}
	for _, tier := range allowed {
		allowedSet[tier] = true
	}
	effective := []string{}
	for _, tier := range requested {
		if allowedSet[tier] {
			effective = append(effective, tier)
		}
	}
	if len(effective) == 0 {
		return "", nil, ErrInvalidPrivacy
	}
	return privacy.ModeForTiers(effective), effective, nil
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
		status.ReservedTokens = m.reservedUse[accountID]
		status.RemainingTokens = consumer.TotalTokenLimit - usage.TotalTokens - status.ReservedTokens
		if status.RemainingTokens < 0 {
			status.RemainingTokens = 0
		}
		status.QuotaExceeded = usage.TotalTokens+status.ReservedTokens >= consumer.TotalTokenLimit
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
			Disabled:        consumer.Disabled,
		}
		for _, hash := range consumer.APIKeyHashes {
			m.apiKeyHashes[hash] = consumer.ID
		}
	}
	for _, provider := range snapshot.Providers {
		privacyMode, privacyTiers, err := providerPrivacy(provider.PrivacyMode, provider.PrivacyTiers)
		if err != nil {
			return err
		}
		m.providers[provider.ID] = Provider{ID: provider.ID, DisplayName: provider.DisplayName, Disabled: provider.Disabled, PrivacyMode: privacyMode, PrivacyTiers: privacyTiers}
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
			Disabled:        consumer.Disabled,
			APIKeyHashes:    hashesForAccount(m.apiKeyHashes, consumer.ID),
		})
	}
	return consumers
}

func (m *Market) persistedProvidersLocked() []persistedProvider {
	providers := make([]persistedProvider, 0, len(m.providers))
	for _, provider := range providerMapValues(m.providers) {
		providers = append(providers, persistedProvider{
			ID:           provider.ID,
			DisplayName:  provider.DisplayName,
			Disabled:     provider.Disabled,
			PrivacyMode:  provider.PrivacyMode,
			PrivacyTiers: provider.PrivacyTiers,
			TokenHashes:  hashesForAccount(m.tokenHashes, provider.ID),
		})
	}
	return providers
}

func (m *Market) releaseReservationLocked(reservation *QuotaReservation) {
	if reservation == nil || reservation.tokens <= 0 {
		return
	}
	current := m.reservedUse[reservation.accountID]
	if current <= reservation.tokens {
		delete(m.reservedUse, reservation.accountID)
		return
	}
	m.reservedUse[reservation.accountID] = current - reservation.tokens
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

func (m *Market) activeConsumerIDLocked(accountID string) (string, error) {
	consumer, ok := m.consumers[accountID]
	if !ok || consumer.Disabled {
		return "", ErrUnauthorizedConsumer
	}
	return accountID, nil
}

func (m *Market) activeProviderIDLocked(accountID string) (string, error) {
	provider, ok := m.providers[accountID]
	if !ok || provider.Disabled {
		return "", ErrUnauthorizedProvider
	}
	return accountID, nil
}

func (m *Market) removeConsumerSecretsLocked(accountID string) {
	for secret, id := range m.apiKeys {
		if id == accountID {
			delete(m.apiKeys, secret)
		}
	}
	for hash, id := range m.apiKeyHashes {
		if id == accountID {
			delete(m.apiKeyHashes, hash)
		}
	}
}

func (m *Market) removeProviderSecretsLocked(accountID string) {
	for secret, id := range m.tokens {
		if id == accountID {
			delete(m.tokens, secret)
		}
	}
	for hash, id := range m.tokenHashes {
		if id == accountID {
			delete(m.tokenHashes, hash)
		}
	}
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

func providerPrivacy(mode string, tiers []string) (string, []string, error) {
	normalized, err := requestedPrivacy(mode, tiers)
	if err != nil {
		return "", nil, err
	}
	return privacy.ModeForTiers(normalized), normalized, nil
}

func requestedPrivacy(mode string, tiers []string) ([]string, error) {
	if len(tiers) > 0 {
		normalized, err := privacy.NormalizeTiers(tiers)
		if err != nil {
			return nil, ErrInvalidPrivacy
		}
		return normalized, nil
	}
	normalized, err := privacy.TiersForMode(mode)
	if err != nil {
		return nil, ErrInvalidPrivacy
	}
	return normalized, nil
}
