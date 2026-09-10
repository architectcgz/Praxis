package modelregistry

import (
	"net/http"
	"testing"

	domainmodel "praxis/internal/domain/model"
)

func TestResolveExecutionModelMapsConfiguredCapabilities(t *testing.T) {
	registry := &Registry{
		client: http.DefaultClient,
		modelsByKey: map[modelKey]registeredModel{
			{ProviderID: "gateway", ModelID: "gpt-test"}: testRegisteredModel(t, ModelConfig{
				ID: "gpt-test", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192,
			}),
		},
		providersByID: map[string]ProviderConfig{
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
	registry := &Registry{modelsByKey: map[modelKey]registeredModel{
		{ProviderID: "gateway", ModelID: "gpt-test"}: testRegisteredModel(t, ModelConfig{
			ID: "gpt-test", ContextWindow: 128000, MaxOutputTokens: 8192,
			ReasoningLevels: []string{"low", "high"}, DefaultReasoningLevel: "high",
		}),
	}}

	selection, err := registry.ResolveModelSelection(" gateway ", " gpt-test ", "")
	if err != nil {
		t.Fatal(err)
	}
	if selection.ProviderID != "gateway" || selection.ModelID != "gpt-test" || selection.ReasoningLevel != "high" {
		t.Fatalf("unexpected model selection: %#v", selection)
	}
}

func TestApplyValidatedConfigReplacesMaterializedDomainModels(t *testing.T) {
	registry := &Registry{modelsPath: t.TempDir() + "/models.json", client: http.DefaultClient}
	initial := testPreparedConfig(t, "high")
	if err := registry.ApplyValidatedConfig(initial); err != nil {
		t.Fatalf("apply initial configuration: %v", err)
	}
	selection, err := registry.ResolveModelSelection("gateway", "gpt-test", "")
	if err != nil {
		t.Fatalf("resolve initial selection: %v", err)
	}
	if selection.ReasoningLevel != "high" {
		t.Fatalf("initial reasoning level = %q, want high", selection.ReasoningLevel)
	}

	updated := testPreparedConfig(t, "low")
	if err := registry.ApplyValidatedConfig(updated); err != nil {
		t.Fatalf("apply updated configuration: %v", err)
	}
	selection, err = registry.ResolveModelSelection("gateway", "gpt-test", "")
	if err != nil {
		t.Fatalf("resolve updated selection: %v", err)
	}
	if selection.ReasoningLevel != "low" {
		t.Fatalf("updated reasoning level = %q, want low", selection.ReasoningLevel)
	}
}

func testRegisteredModel(t *testing.T, config ModelConfig) registeredModel {
	t.Helper()
	model, err := config.DomainModel()
	if err != nil {
		t.Fatalf("materialize domain model: %v", err)
	}
	return registeredModel{config: config, domain: model}
}

func testPreparedConfig(t *testing.T, defaultReasoningLevel string) ValidatedConfig {
	t.Helper()
	prepared, err := Prepare(RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway", ModelConfig{
			ID: "gpt-test", GroupID: "gpt", APIFormat: APIFormatOpenAIResponses,
			ContextWindow: 128000, MaxOutputTokens: 8192,
			ReasoningLevels: []string{"low", "high"}, DefaultReasoningLevel: defaultReasoningLevel,
		})},
	})
	if err != nil {
		t.Fatalf("prepare model configuration: %v", err)
	}
	return prepared
}
