package config

import (
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Coordinator struct {
	ListenAddr      string            `yaml:"listen_addr"`
	APIKeys         []string          `yaml:"api_keys"`
	AdminToken      string            `yaml:"admin_token"`
	DevAdminOpen    bool              `yaml:"dev_admin_open"`
	TLS             ServerTLSConfig   `yaml:"tls"`
	City            CityConfig        `yaml:"city"`
	Settlement      SettlementConfig  `yaml:"settlement"`
	Idempotency     IdempotencyConfig `yaml:"idempotency"`
	Challenges      ChallengeConfig   `yaml:"challenges"`
	Models          ModelConfig       `yaml:"models"`
	Scheduler       SchedulerConfig   `yaml:"scheduler"`
	MaxRequestBytes int64             `yaml:"max_request_bytes"`
}

type ServerTLSConfig struct {
	CertFile         string `yaml:"cert_file"`
	KeyFile          string `yaml:"key_file"`
	NodeClientCAFile string `yaml:"node_client_ca_file"`
}

type CityConfig struct {
	Enabled               bool              `yaml:"enabled"`
	Name                  string            `yaml:"name"`
	RequireProviderTokens bool              `yaml:"require_provider_tokens"`
	UsageStorePath        string            `yaml:"usage_store_path"`
	SQLitePath            string            `yaml:"sqlite_path"`
	Consumers             []ConsumerAccount `yaml:"consumers"`
	Providers             []ProviderAccount `yaml:"providers"`
}

type ConsumerAccount struct {
	ID              string   `yaml:"id"`
	DisplayName     string   `yaml:"display_name"`
	APIKeys         []string `yaml:"api_keys"`
	TotalTokenLimit int64    `yaml:"total_token_limit"`
}

type ProviderAccount struct {
	ID           string   `yaml:"id"`
	DisplayName  string   `yaml:"display_name"`
	Token        string   `yaml:"token"`
	PrivacyMode  string   `yaml:"privacy_mode"`
	PrivacyTiers []string `yaml:"privacy_tiers"`
}

type SettlementConfig struct {
	Enabled                      bool   `yaml:"enabled"`
	ChainPath                    string `yaml:"chain_path"`
	SQLitePath                   string `yaml:"sqlite_path"`
	PricePerThousandTokensMicros int64  `yaml:"price_per_thousand_tokens_micros"`
	ProviderRewardShareBPS       int64  `yaml:"provider_reward_share_bps"`
	TargetLatencyMs              int64  `yaml:"target_latency_ms"`
	LatencyPenaltyBPS            int64  `yaml:"latency_penalty_bps"`
}

type IdempotencyConfig struct {
	SQLitePath string   `yaml:"sqlite_path"`
	TTL        Duration `yaml:"ttl"`
}

type ChallengeConfig struct {
	Enabled          bool     `json:"enabled,omitempty" yaml:"enabled"`
	Path             string   `json:"path,omitempty" yaml:"path"`
	AutoRun          bool     `json:"auto_run,omitempty" yaml:"auto_run"`
	Interval         Duration `json:"interval,omitempty" yaml:"interval"`
	Timeout          Duration `json:"timeout,omitempty" yaml:"timeout"`
	Model            string   `json:"model,omitempty" yaml:"model"`
	ProviderID       string   `json:"provider_id,omitempty" yaml:"provider_id"`
	PrivacyTier      string   `json:"privacy_tier,omitempty" yaml:"privacy_tier"`
	Prompt           string   `json:"prompt,omitempty" yaml:"prompt"`
	ExpectedContains string   `json:"expected_contains,omitempty" yaml:"expected_contains"`
	MaxTokens        int      `json:"max_tokens,omitempty" yaml:"max_tokens"`
}

type ModelConfig struct {
	Aliases []ModelAlias `yaml:"aliases"`
}

type ModelAlias struct {
	ID            string   `yaml:"id"`
	Target        string   `yaml:"target"`
	DisplayName   string   `yaml:"display_name"`
	Description   string   `yaml:"description"`
	Tags          []string `yaml:"tags"`
	ContextWindow int      `yaml:"context_window"`
}

type SchedulerConfig struct {
	MaxQueuePenalty int      `yaml:"max_queue_penalty"`
	RequestTimeout  Duration `yaml:"request_timeout"`
}

type NodeAgent struct {
	NodeID            string          `yaml:"node_id"`
	ProviderID        string          `yaml:"provider_id"`
	ProviderToken     string          `yaml:"provider_token"`
	PublicName        string          `yaml:"public_name"`
	City              string          `yaml:"city"`
	PrivacyMode       string          `yaml:"privacy_mode"`
	PrivacyTiers      []string        `yaml:"privacy_tiers"`
	CoordinatorURL    string          `yaml:"coordinator_url"`
	TLS               ClientTLSConfig `yaml:"tls"`
	Backend           BackendConfig   `yaml:"backend"`
	Hardware          HardwareConfig  `yaml:"hardware"`
	OllamaURL         string          `yaml:"ollama_url"`
	Models            []string        `yaml:"models"`
	HeartbeatInterval Duration        `yaml:"heartbeat_interval"`
	MaxConcurrent     int             `yaml:"max_concurrent"`
}

type BackendConfig struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
}

type HardwareConfig struct {
	Kind         string   `yaml:"kind"`
	Vendor       string   `yaml:"vendor"`
	Model        string   `yaml:"model"`
	SoC          string   `yaml:"soc"`
	Accelerators []string `yaml:"accelerators"`
	PowerMode    string   `yaml:"power_mode"`
	NetworkMode  string   `yaml:"network_mode"`
}

type ClientTLSConfig struct {
	CAFile             string `yaml:"ca_file"`
	CertFile           string `yaml:"cert_file"`
	KeyFile            string `yaml:"key_file"`
	InsecureSkipVerify bool   `yaml:"insecure_skip_verify"`
}

type Duration struct {
	time.Duration
}

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	if value.Value == "" {
		return nil
	}
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return err
	}
	d.Duration = parsed
	return nil
}

func LoadCoordinator(path string) (Coordinator, error) {
	var cfg Coordinator
	if err := loadYAML(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.ListenAddr == "" {
		cfg.ListenAddr = ":8080"
	}
	return cfg, nil
}

func LoadNodeAgent(path string) (NodeAgent, error) {
	var cfg NodeAgent
	if err := loadYAML(path, &cfg); err != nil {
		return cfg, err
	}
	if cfg.CoordinatorURL == "" {
		cfg.CoordinatorURL = "ws://localhost:8080/ws/node"
	}
	if cfg.Backend.Type == "" {
		cfg.Backend.Type = "ollama"
	}
	cfg.Backend.Type = strings.ToLower(cfg.Backend.Type)
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = "http://127.0.0.1:11434"
	}
	if cfg.Backend.URL == "" && cfg.Backend.Type == "ollama" {
		cfg.Backend.URL = cfg.OllamaURL
	}
	if cfg.HeartbeatInterval.Duration == 0 {
		cfg.HeartbeatInterval = Duration{Duration: 5 * time.Second}
	}
	if cfg.MaxConcurrent <= 0 {
		cfg.MaxConcurrent = 1
	}
	return cfg, nil
}

func loadYAML(path string, dst any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, dst)
}
