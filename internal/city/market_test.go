package city

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/privacy"
	"github.com/raym33/mi/internal/protocol"
)

func TestConsumerQuotaAndUsage(t *testing.T) {
	market, err := New(config.CityConfig{
		Enabled: true,
		Consumers: []config.ConsumerAccount{{
			ID:              "studio",
			DisplayName:     "Studio",
			APIKeys:         []string{"sk-test"},
			TotalTokenLimit: 10,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}

	consumerID, err := market.AuthenticateConsumer("sk-test")
	if err != nil {
		t.Fatalf("authenticate consumer: %v", err)
	}
	if consumerID != "studio" {
		t.Fatalf("consumer id = %q, want studio", consumerID)
	}
	if err := market.CheckConsumerQuota(consumerID); err != nil {
		t.Fatalf("quota before usage: %v", err)
	}

	if err := market.Record(consumerID, "provider-a", protocol.InferDone{PromptTokens: 4, OutputTokens: 6}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	if err := market.CheckConsumerQuota(consumerID); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("quota after usage = %v, want ErrQuotaExceeded", err)
	}

	status := market.ConsumerStatus(consumerID)
	if status.Usage.TotalTokens != 10 {
		t.Fatalf("total tokens = %d, want 10", status.Usage.TotalTokens)
	}
	if !status.QuotaExceeded {
		t.Fatal("quota should be exceeded")
	}
}

func TestUsagePersistsAcrossMarketRestart(t *testing.T) {
	usagePath := filepath.Join(t.TempDir(), "city-usage.json")
	cfg := config.CityConfig{
		Enabled:        true,
		UsageStorePath: usagePath,
		Consumers: []config.ConsumerAccount{{
			ID:      "studio",
			APIKeys: []string{"sk-test"},
		}},
		Providers: []config.ProviderAccount{{
			ID:    "provider-a",
			Token: "pk-test",
		}},
	}

	market, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	if err := market.Record("studio", "provider-a", protocol.InferDone{PromptTokens: 7, OutputTokens: 11}); err != nil {
		t.Fatalf("record usage: %v", err)
	}

	restarted, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("restart market: %v", err)
	}
	status := restarted.ConsumerStatus("studio")
	if status.Usage.PromptTokens != 7 || status.Usage.CompletionTokens != 11 || status.Usage.TotalTokens != 18 {
		t.Fatalf("usage after restart = %+v, want prompt=7 completion=11 total=18", status.Usage)
	}
	snapshot := restarted.Snapshot()
	if len(snapshot.ProviderUsage) != 1 || snapshot.ProviderUsage[0].TotalTokens != 18 {
		t.Fatalf("provider usage after restart = %+v, want total=18", snapshot.ProviderUsage)
	}
}

func TestUsagePersistsAcrossSQLiteMarketRestart(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "city.db")
	cfg := config.CityConfig{
		Enabled:               true,
		RequireProviderTokens: true,
		SQLitePath:            sqlitePath,
		Consumers: []config.ConsumerAccount{{
			ID:      "studio",
			APIKeys: []string{"sk-test"},
		}},
		Providers: []config.ProviderAccount{{
			ID:    "provider-a",
			Token: "pk-test",
		}},
	}

	market, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	if err := market.Record("studio", "provider-a", protocol.InferDone{PromptTokens: 7, OutputTokens: 11}); err != nil {
		t.Fatalf("record usage: %v", err)
	}
	var journalMode string
	if err := market.db.QueryRow("PRAGMA journal_mode").Scan(&journalMode); err != nil {
		t.Fatalf("journal mode: %v", err)
	}
	if journalMode != "wal" {
		t.Fatalf("journal mode = %q, want wal", journalMode)
	}
	if err := market.Close(); err != nil {
		t.Fatalf("close market: %v", err)
	}

	restarted, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("restart market: %v", err)
	}
	defer restarted.Close()
	consumerID, err := restarted.AuthenticateConsumer("sk-test")
	if err != nil || consumerID != "studio" {
		t.Fatalf("authenticate restarted consumer = %q, %v", consumerID, err)
	}
	providerID, err := restarted.AuthenticateProvider(protocol.Register{ProviderToken: "pk-test"})
	if err != nil || providerID != "provider-a" {
		t.Fatalf("authenticate restarted provider = %q, %v", providerID, err)
	}
	status := restarted.ConsumerStatus("studio")
	if status.Usage.PromptTokens != 7 || status.Usage.CompletionTokens != 11 || status.Usage.TotalTokens != 18 {
		t.Fatalf("usage after restart = %+v, want prompt=7 completion=11 total=18", status.Usage)
	}
}

func TestDynamicEnrollmentPersistsHashedSecrets(t *testing.T) {
	usagePath := filepath.Join(t.TempDir(), "city-state.json")
	cfg := config.CityConfig{
		Enabled:               true,
		RequireProviderTokens: true,
		UsageStorePath:        usagePath,
	}

	market, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	consumer, err := market.CreateConsumer(CreateConsumerInput{
		ID:              "Studio-New",
		DisplayName:     "Studio New",
		TotalTokenLimit: 123,
	})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	provider, err := market.CreateProvider(CreateProviderInput{ID: "Provider-New", DisplayName: "Provider New"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	if !strings.HasPrefix(consumer.APIKey, "sk-mi-") {
		t.Fatalf("consumer api key = %q, want sk-mi- prefix", consumer.APIKey)
	}
	if !strings.HasPrefix(provider.ProviderToken, "pk-mi-") {
		t.Fatalf("provider token = %q, want pk-mi- prefix", provider.ProviderToken)
	}
	consumerID, err := market.AuthenticateConsumer(consumer.APIKey)
	if err != nil || consumerID != "studio-new" {
		t.Fatalf("authenticate dynamic consumer = %q, %v", consumerID, err)
	}
	providerID, err := market.AuthenticateProvider(protocol.Register{NodeID: "node-a", ProviderToken: provider.ProviderToken})
	if err != nil || providerID != "provider-new" {
		t.Fatalf("authenticate dynamic provider = %q, %v", providerID, err)
	}

	state, err := os.ReadFile(usagePath)
	if err != nil {
		t.Fatalf("read state: %v", err)
	}
	if strings.Contains(string(state), consumer.APIKey) || strings.Contains(string(state), provider.ProviderToken) {
		t.Fatalf("state file contains plaintext secret: %s", string(state))
	}

	restarted, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("restart market: %v", err)
	}
	consumerID, err = restarted.AuthenticateConsumer(consumer.APIKey)
	if err != nil || consumerID != "studio-new" {
		t.Fatalf("authenticate restarted consumer = %q, %v", consumerID, err)
	}
	providerID, err = restarted.AuthenticateProvider(protocol.Register{NodeID: "node-a", ProviderToken: provider.ProviderToken})
	if err != nil || providerID != "provider-new" {
		t.Fatalf("authenticate restarted provider = %q, %v", providerID, err)
	}

	if _, err := restarted.CreateConsumer(CreateConsumerInput{ID: "studio-new"}); !errors.Is(err, ErrAccountExists) {
		t.Fatalf("duplicate consumer = %v, want ErrAccountExists", err)
	}
	if _, err := restarted.CreateProvider(CreateProviderInput{ID: "bad account"}); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("invalid provider = %v, want ErrInvalidAccount", err)
	}
}

func TestDynamicEnrollmentPersistsHashedSecretsInSQLite(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "city.db")
	cfg := config.CityConfig{
		Enabled:               true,
		RequireProviderTokens: true,
		SQLitePath:            sqlitePath,
	}

	market, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	consumer, err := market.CreateConsumer(CreateConsumerInput{ID: "studio"})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	provider, err := market.CreateProvider(CreateProviderInput{ID: "provider-a"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	if err := market.Close(); err != nil {
		t.Fatalf("close market: %v", err)
	}

	data, err := os.ReadFile(sqlitePath)
	if err != nil {
		t.Fatalf("read sqlite file: %v", err)
	}
	if strings.Contains(string(data), consumer.APIKey) || strings.Contains(string(data), provider.ProviderToken) {
		t.Fatalf("sqlite file contains plaintext secret")
	}

	restarted, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("restart market: %v", err)
	}
	defer restarted.Close()
	if consumerID, err := restarted.AuthenticateConsumer(consumer.APIKey); err != nil || consumerID != "studio" {
		t.Fatalf("authenticate restarted consumer = %q, %v", consumerID, err)
	}
	if providerID, err := restarted.AuthenticateProvider(protocol.Register{ProviderToken: provider.ProviderToken}); err != nil || providerID != "provider-a" {
		t.Fatalf("authenticate restarted provider = %q, %v", providerID, err)
	}
}

func TestRotationAndDisablePersist(t *testing.T) {
	usagePath := filepath.Join(t.TempDir(), "city-state.json")
	cfg := config.CityConfig{
		Enabled:               true,
		RequireProviderTokens: true,
		UsageStorePath:        usagePath,
	}

	market, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	consumer, err := market.CreateConsumer(CreateConsumerInput{ID: "studio"})
	if err != nil {
		t.Fatalf("create consumer: %v", err)
	}
	provider, err := market.CreateProvider(CreateProviderInput{ID: "provider-a"})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}

	rotatedConsumer, err := market.RotateConsumerKey("studio")
	if err != nil {
		t.Fatalf("rotate consumer: %v", err)
	}
	if _, err := market.AuthenticateConsumer(consumer.APIKey); !errors.Is(err, ErrUnauthorizedConsumer) {
		t.Fatalf("old consumer key auth = %v, want ErrUnauthorizedConsumer", err)
	}
	if consumerID, err := market.AuthenticateConsumer(rotatedConsumer.APIKey); err != nil || consumerID != "studio" {
		t.Fatalf("new consumer key auth = %q, %v", consumerID, err)
	}

	rotatedProvider, err := market.RotateProviderToken("provider-a")
	if err != nil {
		t.Fatalf("rotate provider: %v", err)
	}
	if _, err := market.AuthenticateProvider(protocol.Register{ProviderToken: provider.ProviderToken}); !errors.Is(err, ErrUnauthorizedProvider) {
		t.Fatalf("old provider token auth = %v, want ErrUnauthorizedProvider", err)
	}
	if providerID, err := market.AuthenticateProvider(protocol.Register{ProviderToken: rotatedProvider.ProviderToken}); err != nil || providerID != "provider-a" {
		t.Fatalf("new provider token auth = %q, %v", providerID, err)
	}

	disabledConsumer, err := market.DisableConsumer("studio")
	if err != nil {
		t.Fatalf("disable consumer: %v", err)
	}
	if !disabledConsumer.Disabled {
		t.Fatal("consumer should be disabled")
	}
	if _, err := market.AuthenticateConsumer(rotatedConsumer.APIKey); !errors.Is(err, ErrUnauthorizedConsumer) {
		t.Fatalf("disabled consumer auth = %v, want ErrUnauthorizedConsumer", err)
	}
	if _, err := market.RotateConsumerKey("studio"); !errors.Is(err, ErrAccountDisabled) {
		t.Fatalf("rotate disabled consumer = %v, want ErrAccountDisabled", err)
	}

	disabledProvider, err := market.DisableProvider("provider-a")
	if err != nil {
		t.Fatalf("disable provider: %v", err)
	}
	if !disabledProvider.Disabled {
		t.Fatal("provider should be disabled")
	}
	if _, err := market.AuthenticateProvider(protocol.Register{ProviderToken: rotatedProvider.ProviderToken}); !errors.Is(err, ErrUnauthorizedProvider) {
		t.Fatalf("disabled provider auth = %v, want ErrUnauthorizedProvider", err)
	}

	restarted, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("restart market: %v", err)
	}
	if _, err := restarted.AuthenticateConsumer(rotatedConsumer.APIKey); !errors.Is(err, ErrUnauthorizedConsumer) {
		t.Fatalf("restarted disabled consumer auth = %v, want ErrUnauthorizedConsumer", err)
	}
	if _, err := restarted.AuthenticateProvider(protocol.Register{ProviderToken: rotatedProvider.ProviderToken}); !errors.Is(err, ErrUnauthorizedProvider) {
		t.Fatalf("restarted disabled provider auth = %v, want ErrUnauthorizedProvider", err)
	}
}

func TestProviderTokenRequired(t *testing.T) {
	market, err := New(config.CityConfig{
		Enabled:               true,
		RequireProviderTokens: true,
		Providers: []config.ProviderAccount{{
			ID:    "provider-a",
			Token: "pk-test",
		}},
	}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}

	if _, err := market.AuthenticateProvider(protocol.Register{NodeID: "node-a"}); !errors.Is(err, ErrUnauthorizedProvider) {
		t.Fatalf("missing provider token = %v, want ErrUnauthorizedProvider", err)
	}
	providerID, err := market.AuthenticateProvider(protocol.Register{NodeID: "node-a", ProviderToken: "pk-test"})
	if err != nil {
		t.Fatalf("authenticate provider: %v", err)
	}
	if providerID != "provider-a" {
		t.Fatalf("provider id = %q, want provider-a", providerID)
	}
}

func TestProviderPrivacyPolicyLimitsNodeClaims(t *testing.T) {
	market, err := New(config.CityConfig{
		Enabled:               true,
		RequireProviderTokens: true,
		Providers: []config.ProviderAccount{{
			ID:          "rented-provider",
			Token:       "pk-test",
			PrivacyMode: privacy.Public,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}

	providerID, err := market.AuthenticateProvider(protocol.Register{ProviderToken: "pk-test"})
	if err != nil {
		t.Fatalf("authenticate provider: %v", err)
	}
	mode, tiers, err := market.EnforceProviderPrivacy(providerID, privacy.Private, nil)
	if err != nil {
		t.Fatalf("enforce provider privacy: %v", err)
	}
	if mode != privacy.Public || len(tiers) != 1 || tiers[0] != privacy.Public {
		t.Fatalf("effective privacy = mode %q tiers %v, want public only", mode, tiers)
	}
	if _, _, err := market.EnforceProviderPrivacy(providerID, "", []string{privacy.Private}); !errors.Is(err, ErrInvalidPrivacy) {
		t.Fatalf("private-only node claim = %v, want ErrInvalidPrivacy", err)
	}
}

func TestProviderPrivacyPolicyPersists(t *testing.T) {
	usagePath := filepath.Join(t.TempDir(), "city-state.json")
	cfg := config.CityConfig{
		Enabled:               true,
		RequireProviderTokens: true,
		UsageStorePath:        usagePath,
	}
	market, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	created, err := market.CreateProvider(CreateProviderInput{ID: "provider-a", PrivacyMode: privacy.Public})
	if err != nil {
		t.Fatalf("create provider: %v", err)
	}
	restarted, err := New(cfg, nil)
	if err != nil {
		t.Fatalf("restart market: %v", err)
	}
	providerID, err := restarted.AuthenticateProvider(protocol.Register{ProviderToken: created.ProviderToken})
	if err != nil {
		t.Fatalf("authenticate restarted provider: %v", err)
	}
	mode, tiers, err := restarted.EnforceProviderPrivacy(providerID, privacy.Private, nil)
	if err != nil {
		t.Fatalf("enforce restarted privacy: %v", err)
	}
	if mode != privacy.Public || len(tiers) != 1 || tiers[0] != privacy.Public {
		t.Fatalf("effective privacy after restart = mode %q tiers %v, want public only", mode, tiers)
	}
}

func TestQuotaReservationsPreventConcurrentOverspend(t *testing.T) {
	market, err := New(config.CityConfig{
		Enabled: true,
		Consumers: []config.ConsumerAccount{{
			ID:              "studio",
			APIKeys:         []string{"sk-test"},
			TotalTokenLimit: 100,
		}},
	}, nil)
	if err != nil {
		t.Fatalf("new market: %v", err)
	}
	first, err := market.ReserveConsumerQuota("studio", 80)
	if err != nil {
		t.Fatalf("first reservation: %v", err)
	}
	if _, err := market.ReserveConsumerQuota("studio", 30); !errors.Is(err, ErrQuotaExceeded) {
		t.Fatalf("second reservation = %v, want ErrQuotaExceeded", err)
	}
	status := market.ConsumerStatus("studio")
	if status.ReservedTokens != 80 || status.RemainingTokens != 20 {
		t.Fatalf("status with reservation = %+v, want reserved=80 remaining=20", status)
	}
	if err := market.RecordReserved(first, "studio", "provider-a", protocol.InferDone{PromptTokens: 10, OutputTokens: 20}); err != nil {
		t.Fatalf("record reserved: %v", err)
	}
	status = market.ConsumerStatus("studio")
	if status.ReservedTokens != 0 || status.Usage.TotalTokens != 30 || status.RemainingTokens != 70 {
		t.Fatalf("status after record = %+v, want used=30 remaining=70", status)
	}
}
