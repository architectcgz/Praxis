package modelregistry

import (
	"testing"

	modelconfig "praxis/internal/core/model/config"
)

func TestResolvedTaskConfigIsIndependentOfCatalogChanges(t *testing.T) {
	provider := modelconfig.Provider{
		ID:      "provider",
		BaseURL: "https://example.com",
		Models:  []modelconfig.Model{{
			ID:                    "model",
			APIFormat:             modelconfig.APIFormatOpenAIChatCompletions,
			ContextWindow:         8192,
			MaxOutputTokens:       1024,
			ReasoningLevels:       []string{"off", "high"},
			DefaultReasoningLevel: "off",
		}},
	}
	models, providers := buildIndexes([]modelconfig.Provider{provider})
	registry := &Registry{modelsByKey: models, providersByID: providers}
	selected, err := registry.ResolveTaskModel("provider", "model", "")
	if err != nil {
		t.Fatal(err)
	}
	provider.Models[0].DefaultReasoningLevel = "high"
	provider.Models[0].ContextWindow = 16384
	provider.BaseURL = "https://changed.example.com"
	models, providers = buildIndexes([]modelconfig.Provider{provider})
	registry.ReplaceFrom(&Registry{modelsByKey: models, providersByID: providers})
	next, err := registry.ResolveTaskModel("provider", "model", "")
	if err != nil {
		t.Fatal(err)
	}
	if selected.ReasoningLevel != "off" || selected.ContextWindow != 8192 || selected.BaseURL != "https://example.com" ||
		next.ReasoningLevel != "high" || next.ContextWindow != 16384 || next.BaseURL != provider.BaseURL {
		t.Fatalf("目录变化影响了已选 Task 配置: %+v, %+v", selected, next)
	}
	selected.ReasoningLevels[0] = "changed"
	clone := next.Clone()
	clone.ReasoningLevels[0] = "changed"
	latest, err := registry.ResolveTaskModel("provider", "model", "")
	if err != nil || latest.ReasoningLevels[0] != "off" || next.ReasoningLevels[0] != "off" {
		t.Fatalf("配置列表未独立复制: %+v, %v", latest, err)
	}
}
