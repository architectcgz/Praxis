package modelregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	modelconfig "praxis/internal/core/model/config"
	"testing"
)

func TestCredentialsRetainExactKeyBytes(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "auth.json")
	registry := &Registry{credentialsPath: path, credentials: ProviderCredentials{}, providersByID: map[string]modelconfig.Provider{"provider": {ID: "provider"}}}
	key := " key with spaces \n"
	if err := registry.SetProviderKey("provider", key); err != nil {
		t.Fatal(err)
	}
	document, err := readCredentialsFile(path)
	if err != nil {
		t.Fatal(err)
	}
	validated, err := validateCredentials(document)
	if err != nil || validated["provider"].Key != key || registry.ProviderKey("provider") != key {
		t.Fatal("credential bytes changed", err)
	}
	for _, document := range []ProviderCredentials{
		{" provider ": {Type: CredentialTypeAPIKey, Key: "key"}},
		{"provider": {Type: CredentialType(" api_key "), Key: "key"}},
	} {
		if _, err := validateCredentials(document); err == nil {
			t.Fatal("noncanonical credential accepted")
		}
	}
	if err := registry.SetProviderKey("provider", ""); err != nil {
		t.Fatal(err)
	}
	encoded, _ := os.ReadFile(path)
	var stored ProviderCredentials
	if err := json.Unmarshal(encoded, &stored); err != nil || len(stored) != 0 {
		t.Fatal(stored, err)
	}
}
