package modelregistry

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSetProviderKeyWritesSeparateAuthDocument(t *testing.T) {
	directory := t.TempDir()
	modelsPath := filepath.Join(directory, "models.json")
	credentialsPath := filepath.Join(directory, "auth.json")
	registry := &Registry{
		modelsPath: modelsPath, credentialsPath: credentialsPath,
		client: http.DefaultClient, credentials: ProviderCredentials{},
	}
	if err := registry.ApplyConfig(RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{testProvider("gateway", ModelConfig{
			ID: "gpt-test", GroupID: "gpt", APIFormat: APIFormatOpenAIResponses, ContextWindow: 128000, MaxOutputTokens: 8192,
		})},
	}); err != nil {
		t.Fatalf("apply model configuration: %v", err)
	}

	if err := registry.SetProviderKey("gateway", "test-key"); err != nil {
		t.Fatalf("set provider key: %v", err)
	}
	if key := registry.ProviderKey("gateway"); key != "test-key" {
		t.Fatalf("provider key = %q, want test-key", key)
	}

	modelsPayload, err := os.ReadFile(modelsPath)
	if err != nil {
		t.Fatalf("read models document: %v", err)
	}
	if strings.Contains(string(modelsPayload), "test-key") {
		t.Fatalf("models document contains a secret: %s", modelsPayload)
	}
	if strings.Contains(string(modelsPayload), "\"credential\"") {
		t.Fatalf("models document contains a credential field: %s", modelsPayload)
	}
	credential, exists := readAuthDocument(t, credentialsPath)["gateway"]
	if !exists || credential.Type != CredentialTypeAPIKey || credential.Key != "test-key" {
		t.Fatalf("stored credential = %#v, want api key credential", credential)
	}

	if err := registry.SetProviderKey("gateway", ""); err != nil {
		t.Fatalf("clear provider key: %v", err)
	}
	if registry.HasProviderKey("gateway") {
		t.Fatal("expected cleared provider key to be absent")
	}
	if _, exists := readAuthDocument(t, credentialsPath)["gateway"]; exists {
		t.Fatal("expected auth document to drop the cleared credential")
	}
}

func TestApplyConfigPrunesCredentialOfRemovedProvider(t *testing.T) {
	directory := t.TempDir()
	registry := &Registry{
		modelsPath: filepath.Join(directory, "models.json"), credentialsPath: filepath.Join(directory, "auth.json"),
		client: http.DefaultClient, credentials: ProviderCredentials{},
	}
	if err := registry.ApplyConfig(RegistryConfig{
		Groups: testGroups(),
		Providers: []ProviderConfig{
			testProvider("gateway"), testProvider("secondary"),
		},
	}); err != nil {
		t.Fatalf("apply initial configuration: %v", err)
	}
	if err := registry.SetProviderKey("gateway", "key-one"); err != nil {
		t.Fatalf("set gateway key: %v", err)
	}
	if err := registry.SetProviderKey("secondary", "key-two"); err != nil {
		t.Fatalf("set secondary key: %v", err)
	}

	if err := registry.ApplyConfig(RegistryConfig{
		Groups:    testGroups(),
		Providers: []ProviderConfig{testProvider("gateway")},
	}); err != nil {
		t.Fatalf("apply reduced configuration: %v", err)
	}
	if key := registry.ProviderKey("secondary"); key != "" {
		t.Fatalf("removed provider key = %q, want empty", key)
	}
	if key := registry.ProviderKey("gateway"); key != "key-one" {
		t.Fatalf("retained provider key = %q, want key-one", key)
	}
	stored := readAuthDocument(t, registry.credentialsPath)
	if len(stored) != 1 || stored["gateway"].Key != "key-one" {
		t.Fatalf("stored credentials = %#v, want only gateway key", stored)
	}
}

func TestLoadSeparatesCredentialFromModelConfiguration(t *testing.T) {
	directory := t.TempDir()
	modelsPath := filepath.Join(directory, "models.json")
	credentialsPath := filepath.Join(directory, "auth.json")
	registry := &Registry{
		modelsPath: modelsPath, credentialsPath: credentialsPath,
		client: http.DefaultClient, credentials: ProviderCredentials{},
	}
	if err := registry.ApplyConfig(RegistryConfig{
		Groups:    testGroups(),
		Providers: []ProviderConfig{testProvider("gateway")},
	}); err != nil {
		t.Fatalf("apply model configuration: %v", err)
	}
	if err := registry.SetProviderKey("gateway", "test-key"); err != nil {
		t.Fatalf("set provider key: %v", err)
	}

	loaded, err := Load(modelsPath, credentialsPath, http.DefaultClient)
	if err != nil {
		t.Fatalf("load registry: %v", err)
	}
	if key := loaded.ProviderKey("gateway"); key != "test-key" {
		t.Fatalf("loaded provider key = %q, want test-key", key)
	}
	if len(loaded.Config().Providers) != 1 {
		t.Fatalf("loaded providers = %#v, want one provider", loaded.Config().Providers)
	}
}

func TestReadCredentialsFileCreatesEmptyDocument(t *testing.T) {
	path := filepath.Join(t.TempDir(), "auth.json")
	document, err := readCredentialsFile(path)
	if err != nil {
		t.Fatalf("read missing auth document: %v", err)
	}
	if len(document) != 0 {
		t.Fatalf("missing auth document = %#v, want empty", document)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created auth document: %v", err)
	}
	if strings.TrimSpace(string(payload)) != "{}" {
		t.Fatalf("created auth document = %q, want empty object", payload)
	}
}

func TestNormalizeCredentialsRejectsInvalidRecords(t *testing.T) {
	cases := []struct {
		name     string
		document ProviderCredentials
	}{
		{name: "invalid provider id", document: ProviderCredentials{"Bad ID": {Type: CredentialTypeAPIKey, Key: "key"}}},
		{name: "unsupported type", document: ProviderCredentials{"gateway": {Type: "oauth", Key: "key"}}},
		{name: "empty key", document: ProviderCredentials{"gateway": {Type: CredentialTypeAPIKey, Key: "  "}}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, err := normalizeCredentials(testCase.document); err == nil {
				t.Fatal("expected invalid auth document to fail validation")
			}
		})
	}
}

func readAuthDocument(t *testing.T, path string) ProviderCredentials {
	t.Helper()
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read auth document: %v", err)
	}
	var document ProviderCredentials
	if err := json.Unmarshal(payload, &document); err != nil {
		t.Fatalf("decode auth document: %v", err)
	}
	return document
}
