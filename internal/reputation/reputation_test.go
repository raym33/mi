package reputation

import (
	"testing"
	"time"

	"github.com/raym33/mi/internal/city"
	"github.com/raym33/mi/internal/scheduler"
	"github.com/raym33/mi/internal/settlement"
)

func TestBuildRanksHealthyProviders(t *testing.T) {
	report := Build(
		city.Snapshot{Providers: []city.Provider{
			{ID: "good", DisplayName: "Good Provider", PrivacyMode: "private"},
			{ID: "bad", DisplayName: "Bad Provider", PrivacyMode: "public"},
			{ID: "disabled", Disabled: true},
		}},
		[]scheduler.NodeView{
			{ID: "node-good", ProviderID: "good", Healthy: true, MaxConcurrent: 2, Models: []string{"m"}},
			{ID: "node-bad", ProviderID: "bad", Healthy: true, InCooldown: true, ErrorStreak: 2, MaxConcurrent: 1, LastSeen: time.Now()},
		},
		settlement.Snapshot{ProviderBalances: []settlement.Balance{
			{AccountID: "good", Events: 12, TotalTokens: 5000, AverageLatencyMs: 250, RewardMicros: 3500},
			{AccountID: "bad", Events: 1, TotalTokens: 100, AverageLatencyMs: 2500, RewardMicros: 70, PenaltyMicros: 500},
		}},
	)
	if len(report.Providers) != 3 {
		t.Fatalf("providers = %d, want 3", len(report.Providers))
	}
	if report.Providers[0].ProviderID != "good" || report.Providers[0].Score <= report.Providers[1].Score {
		t.Fatalf("ranking = %+v, want good provider first", report.Providers)
	}
	disabled := findProvider(report, "disabled")
	if disabled.Score != 0 || disabled.Grade != "F" {
		t.Fatalf("disabled provider = %+v, want zero score/F", disabled)
	}
	bad := findProvider(report, "bad")
	if len(bad.Notes) == 0 || bad.PenaltyMicros != 500 || bad.AverageLatencyMs != 2500 {
		t.Fatalf("bad provider = %+v, want penalty/latency risk notes", bad)
	}
}

func findProvider(report Report, providerID string) ProviderReputation {
	for _, provider := range report.Providers {
		if provider.ProviderID == providerID {
			return provider
		}
	}
	return ProviderReputation{}
}
