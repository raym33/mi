package modelcatalog

import (
	"testing"

	"github.com/raym33/mi/internal/config"
)

func TestResolveAndVisibleModels(t *testing.T) {
	catalog := New(config.ModelConfig{Aliases: []config.ModelAlias{{
		ID:          "fast",
		Target:      "llama3.1:8b",
		DisplayName: "Fast",
	}}})

	resolved := catalog.Resolve("fast")
	if !resolved.IsAlias || resolved.Target != "llama3.1:8b" {
		t.Fatalf("resolved = %+v, want alias to llama3.1:8b", resolved)
	}
	direct := catalog.Resolve("qwen")
	if direct.IsAlias || direct.Target != "qwen" {
		t.Fatalf("direct = %+v, want concrete qwen", direct)
	}

	visible := catalog.VisibleModelIDs([]string{"llama3.1:8b"})
	if len(visible) != 2 || visible[0] != "fast" || visible[1] != "llama3.1:8b" {
		t.Fatalf("visible = %+v, want fast + concrete", visible)
	}
}

func TestCatalogAvailability(t *testing.T) {
	catalog := New(config.ModelConfig{Aliases: []config.ModelAlias{{
		ID:     "code",
		Target: "qwen2.5-coder:7b",
		Tags:   []string{"coding"},
	}}})

	resp := catalog.Catalog([]string{"llama3.1:8b"})
	if len(resp.Data) != 2 {
		t.Fatalf("catalog len = %d, want 2", len(resp.Data))
	}
	if resp.Data[0].ID != "code" || resp.Data[0].Available {
		t.Fatalf("alias entry = %+v, want unavailable code alias", resp.Data[0])
	}
	if resp.Data[1].ID != "llama3.1:8b" || !resp.Data[1].Available {
		t.Fatalf("concrete entry = %+v, want available concrete", resp.Data[1])
	}
}
