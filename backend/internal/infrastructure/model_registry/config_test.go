package modelregistry

import (
	"os"
	"path/filepath"
	"testing"
)

func testGroups() []GroupConfig {
	return []GroupConfig{{ID: "gpt", DisplayName: "GPT"}, {ID: "claude", DisplayName: "Claude"}}
}

func testProvider(id string, models ...ModelConfig) ProviderConfig {
	return ProviderConfig{
		ID: id, DisplayName: id, BaseURL: "https://gateway.example.com",
		Models:     models,
		Credential: &CredentialRecord{Type: CredentialTypeAPIKey, Key: "test-key"},
	}
}

func TestProviderOwnsModelsAndGroupNormalizesReasoning(t *testing.T) {
	config := RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway", ModelConfig{
			ID: "model", DisplayName: "", GroupID: "gpt", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192,
		})},
	}

	validated, err := Validate(config)
	if err != nil {
		t.Fatalf("validate nested model configuration: %v", err)
	}
	model := validated.Providers[0].Models[0]
	if model.DisplayName != "model" {
		t.Fatalf("model display name = %q, want model", model.DisplayName)
	}
	if len(model.ReasoningLevels) != 0 || model.DefaultReasoningLevel != "" {
		t.Fatalf("missing reasoning configuration must remain unsupported: %#v", model)
	}
	if model.APIFormat != APIFormatOpenAIResponses {
		t.Fatalf("model API format = %q, want %q", model.APIFormat, APIFormatOpenAIResponses)
	}
}

func TestModelsMayUseDifferentAPIFormats(t *testing.T) {
	config := RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway",
			ModelConfig{ID: "gpt", GroupID: "gpt", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192},
			ModelConfig{ID: "claude", GroupID: "claude", APIFormat: APIFormatAnthropicMessages, ContextWindow: 200000, MaxOutputTokens: 8192},
		)},
	}

	validated, err := Validate(config)
	if err != nil {
		t.Fatalf("validate API format configuration: %v", err)
	}
	models := validated.Providers[0].Models
	if models[0].APIFormat != APIFormatOpenAIResponses {
		t.Fatalf("first model format = %q, want %q", models[0].APIFormat, APIFormatOpenAIResponses)
	}
	if models[1].APIFormat != APIFormatAnthropicMessages {
		t.Fatalf("second model format = %q, want %q", models[1].APIFormat, APIFormatAnthropicMessages)
	}
}

func TestValidateRejectsMissingModelAPIFormat(t *testing.T) {
	config := RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{{
			ID: "gateway", DisplayName: "Gateway", BaseURL: "https://gateway.example.com",
			Credential: &CredentialRecord{Type: CredentialTypeAPIKey, Key: "test-key"},
			Models: []ModelConfig{{
				ID: "model", GroupID: "gpt", ContextWindow: 128000, MaxOutputTokens: 8192,
			}},
		}},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected missing model API format to fail validation")
	}
}

func TestValidateRejectsUnknownGroup(t *testing.T) {
	config := RegistryConfig{
		Groups: []GroupConfig{{ID: "gpt", DisplayName: "GPT"}},
		Providers: []ProviderConfig{testProvider("gateway", ModelConfig{
			ID: "model", GroupID: "claude", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192,
		})},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected unknown group reference to fail")
	}
}

func TestValidateRejectsDuplicateModelWithinProvider(t *testing.T) {
	config := RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway",
			ModelConfig{ID: "shared", GroupID: "gpt", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192},
			ModelConfig{ID: "shared", GroupID: "claude", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192},
		)},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected duplicate provider model to fail")
	}
}

func TestSameModelIDMayBeUsedByDifferentProviders(t *testing.T) {
	config := RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{
			testProvider("one", ModelConfig{ID: "shared", GroupID: "gpt", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192}),
			testProvider("two", ModelConfig{ID: "shared", GroupID: "gpt", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192}),
		},
	}
	if _, err := Validate(config); err != nil {
		t.Fatalf("validate same model ID across providers: %v", err)
	}
}

func TestProviderProxyURLMustUseHTTPOrHTTPS(t *testing.T) {
	config := RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{{
			ID: "gateway", DisplayName: "Gateway", BaseURL: "https://gateway.example.com",
			ProxyURL: "socks5://127.0.0.1:7897",
		}},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected unsupported proxy protocol to fail validation")
	}
}

func TestReadRegistryConfigRejectsUnknownFields(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	content := `{"groups":[],"providers":[],"profiles":{}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write models config: %v", err)
	}
	if _, err := readModelConfigFile(path); err == nil {
		t.Fatal("expected unknown field to fail")
	}
}

func TestPrepareMaterializesNormalizedDomainModels(t *testing.T) {
	prepared, err := Prepare(RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway", ModelConfig{
			ID: "model", GroupID: "gpt", APIFormat: APIFormatOpenAIResponses,
			ContextWindow: 128000, MaxOutputTokens: 8192,
			ReasoningLevels: []string{" low ", "high"}, DefaultReasoningLevel: " low ",
		})},
	})
	if err != nil {
		t.Fatalf("prepare model configuration: %v", err)
	}
	model, exists := prepared.models[modelKey{ProviderID: "gateway", ModelID: "model"}]
	if !exists {
		t.Fatal("expected materialized domain model")
	}
	if model.DefaultReasoningLevel != "low" || len(model.ReasoningLevels) != 2 || model.ReasoningLevels[0] != "low" {
		t.Fatalf("materialized model = %#v", model)
	}
}
