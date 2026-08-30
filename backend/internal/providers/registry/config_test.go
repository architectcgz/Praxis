package registry

import "testing"

func TestProviderNameIsOptional(t *testing.T) {
	config := FileConfig{
		Providers: []ProviderConfig{{
			ID: "gateway", BaseURL: "https://gateway.example.com",
		}},
		Models:   []ModelConfig{},
		Profiles: map[string]ModelReference{},
	}

	if _, err := Validate(config); err != nil {
		t.Fatalf("validate configuration without provider name: %v", err)
	}
}

func TestModelsChooseAPIFormatIndependently(t *testing.T) {
	config := FileConfig{
		Providers: []ProviderConfig{{
			ID: "gateway", ProviderName: "Gateway", BaseURL: "https://gateway.example.com",
		}},
		Models: []ModelConfig{
			{
				ProviderID: "gateway", ModelID: "claude-sonnet",
				APIFormat: APIFormatAnthropicMessages, ContextWindow: 200000, MaxOutputTokens: 8192,
			},
			{
				ProviderID: "gateway", ModelID: "gpt-5",
				APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192,
			},
		},
		Profiles: map[string]ModelReference{},
	}

	validated, err := Validate(config)
	if err != nil {
		t.Fatalf("validate model configuration: %v", err)
	}
	registry := &Registry{
		config:     validated,
		byProvider: map[string]ProviderConfig{"gateway": validated.Providers[0]},
		byModel: map[modelKey]ModelConfig{
			{ProviderID: "gateway", ModelID: "claude-sonnet"}: validated.Models[0],
			{ProviderID: "gateway", ModelID: "gpt-5"}:         validated.Models[1],
		},
		secrets: map[string]string{"gateway": "test-key"},
	}
	for _, modelID := range []string{"claude-sonnet", "gpt-5"} {
		if _, err := registry.Stream("gateway", modelID); err != nil {
			t.Fatalf("create model stream for %s: %v", modelID, err)
		}
	}
}

func TestModelAPIFormatIsRequired(t *testing.T) {
	config := FileConfig{
		Providers: []ProviderConfig{{
			ID: "gateway", BaseURL: "https://gateway.example.com",
		}},
		Models: []ModelConfig{{
			ProviderID: "gateway", ModelID: "model",
			ContextWindow: 128000, MaxOutputTokens: 8192,
		}},
		Profiles: map[string]ModelReference{},
	}

	if _, err := Validate(config); err == nil {
		t.Fatal("expected missing model API format to fail validation")
	}
}

func TestProviderProxyURLMustUseHTTPOrHTTPS(t *testing.T) {
	config := FileConfig{
		Providers: []ProviderConfig{{
			ID: "gateway", BaseURL: "https://gateway.example.com", ProxyURL: "socks5://127.0.0.1:7897",
		}},
		Models:   []ModelConfig{},
		Profiles: map[string]ModelReference{},
	}

	if _, err := Validate(config); err == nil {
		t.Fatal("expected unsupported proxy protocol to fail validation")
	}
}

func TestSameRemoteModelIDMayBeConfiguredForDifferentProviders(t *testing.T) {
	config := FileConfig{
		Providers: []ProviderConfig{
			{ID: "one", BaseURL: "https://one.example.com"},
			{ID: "two", BaseURL: "https://two.example.com"},
		},
		Models: []ModelConfig{
			{ProviderID: "one", ModelID: "shared", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192},
			{ProviderID: "two", ModelID: "shared", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192},
		},
		Profiles: map[string]ModelReference{
			"primary": {ProviderID: "one", ModelID: "shared"},
		},
	}
	if _, err := Validate(config); err != nil {
		t.Fatalf("validate shared model ID across providers: %v", err)
	}
}

func TestDuplicateRemoteModelIDForSameProviderFails(t *testing.T) {
	config := FileConfig{
		Providers: []ProviderConfig{{ID: "gateway", BaseURL: "https://gateway.example.com"}},
		Models: []ModelConfig{
			{ProviderID: "gateway", ModelID: "shared", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192},
			{ProviderID: "gateway", ModelID: "shared", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192},
		},
		Profiles: map[string]ModelReference{},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected duplicate provider/model reference to fail")
	}
}

func TestProfileReferenceUsesProviderAndModelID(t *testing.T) {
	config := FileConfig{
		Providers: []ProviderConfig{{ID: "gateway", BaseURL: "https://gateway.example.com"}},
		Models:    []ModelConfig{{ProviderID: "gateway", ModelID: "shared", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192}},
		Profiles:  map[string]ModelReference{"primary": {ProviderID: "other", ModelID: "shared"}},
	}
	if _, err := Validate(config); err == nil {
		t.Fatal("expected unknown composite profile reference to fail")
	}
}
