package reputation

import (
	"sort"
	"time"

	"github.com/raym33/mi/internal/challenge"
	"github.com/raym33/mi/internal/city"
	"github.com/raym33/mi/internal/scheduler"
	"github.com/raym33/mi/internal/settlement"
)

type Report struct {
	GeneratedAt time.Time            `json:"generated_at"`
	Providers   []ProviderReputation `json:"providers"`
}

type ProviderReputation struct {
	ProviderID           string           `json:"provider_id"`
	DisplayName          string           `json:"display_name,omitempty"`
	PrivacyMode          string           `json:"privacy_mode,omitempty"`
	Disabled             bool             `json:"disabled,omitempty"`
	Score                int              `json:"score"`
	Grade                string           `json:"grade"`
	TotalNodes           int              `json:"total_nodes"`
	HealthyNodes         int              `json:"healthy_nodes"`
	CooldownNodes        int              `json:"cooldown_nodes"`
	ActiveRequests       int              `json:"active_requests"`
	ErrorStreak          int              `json:"error_streak"`
	CompletedEvents      int64            `json:"completed_events"`
	TotalTokens          int64            `json:"total_tokens"`
	AverageLatencyMs     int64            `json:"average_latency_ms,omitempty"`
	Challenges           int64            `json:"challenges,omitempty"`
	ChallengePassRateBPS int64            `json:"challenge_pass_rate_bps,omitempty"`
	ChallengeScore       int              `json:"challenge_score,omitempty"`
	RewardMicros         int64            `json:"reward_micros"`
	PenaltyMicros        int64            `json:"penalty_micros,omitempty"`
	Notes                []string         `json:"notes,omitempty"`
	Nodes                []NodeReputation `json:"nodes,omitempty"`
}

type NodeReputation struct {
	NodeID        string   `json:"node_id"`
	PublicName    string   `json:"public_name,omitempty"`
	Backend       string   `json:"backend,omitempty"`
	DeviceKind    string   `json:"device_kind,omitempty"`
	DeviceVendor  string   `json:"device_vendor,omitempty"`
	DeviceModel   string   `json:"device_model,omitempty"`
	SoC           string   `json:"soc,omitempty"`
	Accelerators  []string `json:"accelerators,omitempty"`
	Healthy       bool     `json:"healthy"`
	InCooldown    bool     `json:"in_cooldown"`
	ErrorStreak   int      `json:"error_streak,omitempty"`
	Active        int      `json:"active"`
	MaxConcurrent int      `json:"max_concurrent"`
	Models        []string `json:"models,omitempty"`
	LastError     string   `json:"last_error,omitempty"`
}

func Build(citySnapshot city.Snapshot, nodes []scheduler.NodeView, settlementSnapshot settlement.Snapshot, challengeSnapshot challenge.Snapshot) Report {
	providers := map[string]*ProviderReputation{}
	for _, provider := range citySnapshot.Providers {
		providers[provider.ID] = &ProviderReputation{
			ProviderID:  provider.ID,
			DisplayName: provider.DisplayName,
			PrivacyMode: provider.PrivacyMode,
			Disabled:    provider.Disabled,
		}
	}
	for _, balance := range settlementSnapshot.ProviderBalances {
		item := providerItem(providers, balance.AccountID)
		item.CompletedEvents = balance.Events
		item.TotalTokens = balance.TotalTokens
		item.AverageLatencyMs = balance.AverageLatencyMs
		item.RewardMicros = balance.RewardMicros
		item.PenaltyMicros = balance.PenaltyMicros
	}
	for _, summary := range challengeSnapshot.Summaries {
		item := providerItem(providers, summary.ProviderID)
		item.Challenges = summary.Challenges
		item.ChallengePassRateBPS = summary.PassRateBPS
		item.ChallengeScore = summary.AverageScore
	}
	for _, node := range nodes {
		providerID := node.ProviderID
		if providerID == "" {
			providerID = node.ID
		}
		item := providerItem(providers, providerID)
		item.TotalNodes++
		item.ActiveRequests += node.Active
		item.ErrorStreak += node.ErrorStreak
		if node.Healthy {
			item.HealthyNodes++
		}
		if node.InCooldown {
			item.CooldownNodes++
		}
		item.Nodes = append(item.Nodes, NodeReputation{
			NodeID:        node.ID,
			PublicName:    node.PublicName,
			Backend:       node.Backend,
			DeviceKind:    node.DeviceKind,
			DeviceVendor:  node.DeviceVendor,
			DeviceModel:   node.DeviceModel,
			SoC:           node.SoC,
			Accelerators:  node.Accelerators,
			Healthy:       node.Healthy,
			InCooldown:    node.InCooldown,
			ErrorStreak:   node.ErrorStreak,
			Active:        node.Active,
			MaxConcurrent: node.MaxConcurrent,
			Models:        node.Models,
			LastError:     node.LastError,
		})
	}
	out := make([]ProviderReputation, 0, len(providers))
	for _, item := range providers {
		item.Score, item.Notes = score(*item)
		item.Grade = grade(item.Score)
		sort.Slice(item.Nodes, func(i, j int) bool { return item.Nodes[i].NodeID < item.Nodes[j].NodeID })
		out = append(out, *item)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Score == out[j].Score {
			return out[i].ProviderID < out[j].ProviderID
		}
		return out[i].Score > out[j].Score
	})
	return Report{GeneratedAt: time.Now().UTC(), Providers: out}
}

func providerItem(items map[string]*ProviderReputation, providerID string) *ProviderReputation {
	if providerID == "" {
		providerID = "unknown"
	}
	item := items[providerID]
	if item == nil {
		item = &ProviderReputation{ProviderID: providerID}
		items[providerID] = item
	}
	return item
}

func score(item ProviderReputation) (int, []string) {
	if item.Disabled {
		return 0, []string{"provider disabled"}
	}
	notes := []string{}
	value := 40
	if item.TotalNodes == 0 {
		notes = append(notes, "no active nodes")
	} else {
		value += 30 * item.HealthyNodes / item.TotalNodes
		if item.CooldownNodes > 0 {
			notes = append(notes, "nodes in cooldown")
			value -= 10 * item.CooldownNodes
		}
	}
	if item.CompletedEvents > 0 {
		value += min(15, int(item.CompletedEvents))
	} else {
		notes = append(notes, "no completed settlement events")
	}
	value += min(10, int(item.TotalTokens/1000))
	if item.ErrorStreak > 0 {
		notes = append(notes, "recent node errors")
		value -= min(25, item.ErrorStreak*5)
	}
	if item.PenaltyMicros > 0 {
		notes = append(notes, "SLA latency penalties applied")
		value -= min(20, int(item.PenaltyMicros/100))
	}
	if item.Challenges > 0 {
		value += min(10, item.ChallengeScore/10)
		if item.ChallengePassRateBPS < 8000 {
			notes = append(notes, "challenge pass rate below target")
			value -= min(25, int((8000-item.ChallengePassRateBPS)/400))
		}
	} else {
		notes = append(notes, "no benchmark challenges recorded")
	}
	if item.TotalNodes > 0 && item.HealthyNodes == 0 {
		notes = append(notes, "all nodes unhealthy")
	}
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return value, notes
}

func grade(score int) string {
	switch {
	case score >= 90:
		return "A"
	case score >= 75:
		return "B"
	case score >= 60:
		return "C"
	case score >= 40:
		return "D"
	default:
		return "F"
	}
}
