package modelcatalog

import (
	"sort"
	"strings"

	"github.com/raym33/mi/internal/config"
)

type Catalog struct {
	aliases map[string]Alias
	ordered []Alias
}

type Alias struct {
	ID            string   `json:"id"`
	Target        string   `json:"target"`
	DisplayName   string   `json:"display_name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
}

type Resolution struct {
	Requested string `json:"requested"`
	Target    string `json:"target"`
	IsAlias   bool   `json:"is_alias"`
}

type CatalogResponse struct {
	Object string         `json:"object"`
	Data   []CatalogModel `json:"data"`
}

type CatalogModel struct {
	ID            string   `json:"id"`
	Object        string   `json:"object"`
	Kind          string   `json:"kind"`
	Target        string   `json:"target,omitempty"`
	DisplayName   string   `json:"display_name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Tags          []string `json:"tags,omitempty"`
	ContextWindow int      `json:"context_window,omitempty"`
	Available     bool     `json:"available"`
}

func New(cfg config.ModelConfig) *Catalog {
	c := &Catalog{aliases: map[string]Alias{}}
	for _, alias := range cfg.Aliases {
		id := strings.TrimSpace(alias.ID)
		target := strings.TrimSpace(alias.Target)
		if id == "" || target == "" {
			continue
		}
		entry := Alias{
			ID:            id,
			Target:        target,
			DisplayName:   strings.TrimSpace(alias.DisplayName),
			Description:   strings.TrimSpace(alias.Description),
			Tags:          append([]string(nil), alias.Tags...),
			ContextWindow: alias.ContextWindow,
		}
		c.aliases[id] = entry
	}
	for _, alias := range c.aliases {
		c.ordered = append(c.ordered, alias)
	}
	sort.Slice(c.ordered, func(i, j int) bool { return c.ordered[i].ID < c.ordered[j].ID })
	return c
}

func (c *Catalog) Resolve(model string) Resolution {
	if c == nil {
		return Resolution{Requested: model, Target: model}
	}
	if alias, ok := c.aliases[model]; ok {
		return Resolution{Requested: model, Target: alias.Target, IsAlias: true}
	}
	return Resolution{Requested: model, Target: model}
}

func (c *Catalog) VisibleModelIDs(available []string) []string {
	availableSet := stringSet(available)
	visible := map[string]bool{}
	for model := range availableSet {
		visible[model] = true
	}
	if c != nil {
		for _, alias := range c.ordered {
			if availableSet[alias.Target] {
				visible[alias.ID] = true
			}
		}
	}
	out := make([]string, 0, len(visible))
	for model := range visible {
		out = append(out, model)
	}
	sort.Strings(out)
	return out
}

func (c *Catalog) Catalog(available []string) CatalogResponse {
	availableSet := stringSet(available)
	seen := map[string]bool{}
	resp := CatalogResponse{Object: "list"}
	if c != nil {
		for _, alias := range c.ordered {
			resp.Data = append(resp.Data, CatalogModel{
				ID:            alias.ID,
				Object:        "model",
				Kind:          "alias",
				Target:        alias.Target,
				DisplayName:   alias.DisplayName,
				Description:   alias.Description,
				Tags:          alias.Tags,
				ContextWindow: alias.ContextWindow,
				Available:     availableSet[alias.Target],
			})
			seen[alias.ID] = true
		}
	}
	for _, model := range sortedKeys(availableSet) {
		if seen[model] {
			continue
		}
		resp.Data = append(resp.Data, CatalogModel{
			ID:        model,
			Object:    "model",
			Kind:      "concrete",
			Available: true,
		})
	}
	return resp
}

func stringSet(values []string) map[string]bool {
	set := map[string]bool{}
	for _, value := range values {
		if value != "" {
			set[value] = true
		}
	}
	return set
}

func sortedKeys(values map[string]bool) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
