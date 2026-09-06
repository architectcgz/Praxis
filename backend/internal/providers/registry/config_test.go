package registry

import (
	"strings"
	"testing"
)

func testGroups() []GroupConfig {
	return []GroupConfig{{ID: "gpt", DisplayName: "GPT"}, {ID: "claude", DisplayName: "Claude"}}
}

func testProvider(id string, format ModelAPIFormat, models ...ModelConfig) ProviderConfig {
	return ProviderConfig{
		ID: id, DisplayName: id, BaseURL: "https://gateway.example.com",
		DefaultAPIFormat: format, Models: models,
		Credential: &CredentialRecord{Type: CredentialTypeAPIKey, Key: "test-key"},
	}
}

func TestProviderOwnsModelsAndGroupNormalizesReasoning(t *testing.T) {
	config := FileConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway", APIFormatOpenAIResponses, ModelConfig{
			ID: "model", DisplayName: "", GroupID: "gpt", ContextWindow: 128000, MaxOutputTokens: 8192,
		})},
		Profiles: map[string]ModelReference{},
	}

	validated, err := Validate(config)
	if err != nil {
		t.Fatalf("validate nested model configuration: %v", err)
	}
	model := validated.Providers[0].Models[0]
	if model.DisplayName != "model" {
		t.Fatalf("model display name = %q, want model", model.DisplayName)
	}
	if !model.Reasoning.Supported {
		t.Fatal("expected missing reasoning configuration to use defaults")
	}
	if got, want := strings.Join(model.Reasoning.Levels, ","), "low,medium,high"; got != want {
		t.Fatalf("default reasoning levels = %q, want %q", got, want)
	}
	if got := EffectiveAPIFormat(validated.Providers[0], model); got != APIFormatOpenAIResponses {
		t.Fatalf("effective API format = %q, want %q", got, APIFormatOpenAIResponses)
	}
}

func TestModelAPIFormatOverrideUsesProviderDefault(t *testing.T) {
	claude := APIFormatAnthropicMessages
	config := FileConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway", APIFormatOpenAIResponses,
			ModelConfig{ID: "gpt", GroupID: "gpt", ContextWindow: 128000, MaxOutputTokens: 8192},
			ModelConfig{ID: "claude", GroupID: "claude", APIFormatOverride: &claude, ContextWindow: 200000, MaxOutputTokens: 8192},
		)},
		Profiles: map[string]ModelReference{},
	}

	validated, err := Validate(config)
	if err != nil {
		t.Fatalf("validate API format configuration: %v", err)
	}
	models := validated.Providers[0].Models
	if got := EffectiveAPIFormat(validated.Providers[0], models[0]); got != APIFormatOpenAIResponses {
		t.Fatalf("default model format = %q, want %q", got, APIFormatOpenAIResponses)
	}
	if got := EffectiveAPIFormat(validated.Providers[0], models[1]); got != APIFormatAnthropicMessages {
		t.Fatalf("overridden model format = %q, want %q", got, APIFormatAnthropicMessages)
	}
}

func TestValidateRejectsUnknownGroup(t *testing.T) {
	config := FileConfig{
		Groups: []GroupConfig{{ID: "gpt", DisplayName: "GPT"}},
		Providers: []ProviderConfig{testProvider("gateway", APIFormatOpenAIResponses, ModelConfig{
			ID: "model", GroupID: "claude", ContextWindow: 128000, MaxOutputTokens: 8192,
		})},
		Profiles: map[string]ModelReference{},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected unknown group reference to fail")
	}
}

func TestValidateRejectsDuplicateModelWithinProvider(t *testing.T) {
	config := FileConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway", APIFormatOpenAIResponses,
			ModelConfig{ID: "shared", GroupID: "gpt", ContextWindow: 128000, MaxOutputTokens: 8192},
			ModelConfig{ID: "shared", GroupID: "claude", ContextWindow: 128000, MaxOutputTokens: 8192},
		)},
		Profiles: map[string]ModelReference{},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected duplicate provider model to fail")
	}
}

func TestSameModelIDMayBeUsedByDifferentProviders(t *testing.T) {
	config := FileConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{
			testProvider("one", APIFormatOpenAIResponses, ModelConfig{ID: "shared", GroupID: "gpt", ContextWindow: 128000, MaxOutputTokens: 8192}),
			testProvider("two", APIFormatOpenAIResponses, ModelConfig{ID: "shared", GroupID: "gpt", ContextWindow: 128000, MaxOutputTokens: 8192}),
		},
		Profiles: map[string]ModelReference{"primary": {ProviderID: "one", ModelID: "shared"}},
	}
	if _, err := Validate(config); err != nil {
		t.Fatalf("validate same model ID across providers: %v", err)
	}
}

func TestProviderProxyURLMustUseHTTPOrHTTPS(t *testing.T) {
	config := FileConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{{
			ID: "gateway", DisplayName: "Gateway", BaseURL: "https://gateway.example.com",
			ProxyURL: "socks5://127.0.0.1:7897", DefaultAPIFormat: APIFormatOpenAIResponses,
		}},
		Profiles: map[string]ModelReference{},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected unsupported proxy protocol to fail validation")
	}
}

func TestProfileReferenceUsesProviderAndModelID(t *testing.T) {
	config := FileConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway", APIFormatOpenAIResponses, ModelConfig{
			ID: "shared", GroupID: "gpt", ContextWindow: 128000, MaxOutputTokens: 8192,
		})},
		Profiles: map[string]ModelReference{"primary": {ProviderID: "other", ModelID: "shared"}},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected unknown composite profile reference to fail")
	}
}
