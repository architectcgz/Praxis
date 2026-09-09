package modelregistry

import (
	"net/http"
	"testing"

	domainmodel "praxis/internal/domain/model"
)

func TestResolveExecutionModelMapsConfiguredCapabilities(t *testing.T) {
	registry := &Registry{
		client: http.DefaultClient,
		byModel: map[modelKey]ModelConfig{
			{ProviderID: "gateway", ModelID: "gpt-test"}: {
				ID: "gpt-test", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192,
			},
		},
		byProvider: map[string]ProviderConfig{
			"gateway": {
				ID: "gateway", BaseURL: "https://gateway.example.com",
				Credential: &CredentialRecord{Type: CredentialTypeAPIKey, Key: "test-key"},
			},
		},
	}

	model, err := registry.ResolveExecutionModel(domainmodel.ModelSelection{
		ProviderID: "gateway", ModelID: "gpt-test",
	})
	if err != nil {
		t.Fatalf("resolve execution model: %v", err)
	}
	if model.Stream == nil {
		t.Fatal("expected execution model stream")
	}
	if model.ContextWindow != 128000 {
		t.Fatalf("context window = %d, want 128000", model.ContextWindow)
	}
	if model.MaxOutputTokens != 8192 {
		t.Fatalf("max output tokens = %d, want 8192", model.MaxOutputTokens)
	}
}

func TestResolveModelSelectionUsesDomainReasoningDefault(t *testing.T) {
	registry := &Registry{byModel: map[modelKey]ModelConfig{
		{ProviderID: "gateway", ModelID: "gpt-test"}: {
			ID: "gpt-test", ContextWindow: 128000, MaxOutputTokens: 8192,
			ReasoningLevels: []string{"low", "high"}, DefaultReasoningLevel: "high",
		},
	}}

	selection, err := registry.ResolveModelSelection("gateway", "gpt-test", "")
	if err != nil {
		t.Fatal(err)
	}
	if selection.ProviderID != "gateway" || selection.ModelID != "gpt-test" || selection.ReasoningLevel != "high" {
		t.Fatalf("unexpected model selection: %#v", selection)
	}
}
