package city

import (
	"errors"
	"testing"

	"github.com/raym33/mi/internal/config"
	"github.com/raym33/mi/internal/protocol"
)

func TestConsumerQuotaAndUsage(t *testing.T) {
	market := New(config.CityConfig{
		Enabled: true,
		Consumers: []config.ConsumerAccount{{
			ID:              "studio",
			DisplayName:     "Studio",
			APIKeys:         []string{"sk-test"},
			TotalTokenLimit: 10,
		}},
	}, nil)

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

	market.Record(consumerID, "provider-a", protocol.InferDone{PromptTokens: 4, OutputTokens: 6})
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

func TestProviderTokenRequired(t *testing.T) {
	market := New(config.CityConfig{
		Enabled:               true,
		RequireProviderTokens: true,
		Providers: []config.ProviderAccount{{
			ID:    "provider-a",
			Token: "pk-test",
		}},
	}, nil)

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
