package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadNodeAgentDefaultsBackend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "node.yaml")
	if err := os.WriteFile(path, []byte(`backend:
  type: "Ollama"
`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	cfg, err := LoadNodeAgent(path)
	if err != nil {
		t.Fatalf("load node agent: %v", err)
	}
	if cfg.Backend.Type != "ollama" || cfg.Backend.URL != "http://127.0.0.1:11434" {
		t.Fatalf("backend = %+v, want normalized ollama default URL", cfg.Backend)
	}
}
