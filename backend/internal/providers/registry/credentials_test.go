package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderKeyIsStoredInNestedCredential(t *testing.T) {
	modelsPath := filepath.Join(t.TempDir(), "models.json")
	registry := &Registry{
		modelsPath: modelsPath,
		config: FileConfig{Groups: testGroups(), Providers: []ProviderConfig{{
			ID: "gateway", DisplayName: "Gateway", BaseURL: "https://gateway.example.com",
			DefaultAPIFormat: APIFormatOpenAIResponses,
		}}},
		byProvider: map[string]ProviderConfig{
			"gateway": {ID: "gateway", DisplayName: "Gateway", BaseURL: "https://gateway.example.com", DefaultAPIFormat: APIFormatOpenAIResponses},
		},
	}

	if err := registry.SetProviderKey("gateway", "test-key"); err != nil {
		t.Fatalf("set provider key: %v", err)
	}
	if key := registry.ProviderKey("gateway"); key != "test-key" {
		t.Fatalf("provider key = %q, want test-key", key)
	}

	payload, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatalf("read model providers file: %v", err)
	}
	var saved FileConfig
	if err := json.Unmarshal(payload, &saved); err != nil {
		t.Fatalf("decode model providers file: %v", err)
	}
	credential := saved.Providers[0].Credential
	if credential == nil || credential.Type != CredentialTypeAPIKey || credential.Key != "test-key" {
		t.Fatalf("saved credential = %#v, want api key credential", credential)
	}
}
