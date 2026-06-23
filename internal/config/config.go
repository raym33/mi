package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Coordinator struct {
	ListenAddr string          `yaml:"listen_addr"`
	APIKeys    []string        `yaml:"api_keys"`
	Scheduler  SchedulerConfig `yaml:"scheduler"`
}

type SchedulerConfig struct {
	MaxQueuePenalty int `yaml:"max_queue_penalty"`
}

type NodeAgent struct {
	NodeID            string   `yaml:"node_id"`
	CoordinatorURL    string   `yaml:"coordinator_url"`
	OllamaURL         string   `yaml:"ollama_url"`
	Models            []string `yaml:"models"`
	HeartbeatInterval Duration `yaml:"heartbeat_interval"`
	MaxConcurrent     int      `yaml:"max_concurrent"`
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
	if cfg.OllamaURL == "" {
		cfg.OllamaURL = "http://localhost:11434"
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
