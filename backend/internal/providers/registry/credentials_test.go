package registry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestProviderKeyIsStoredByProviderID(t *testing.T) {
	secretsPath := filepath.Join(t.TempDir(), "secrets.json")
	registry := &Registry{
		secretsPath: secretsPath,
		secrets:     map[string]string{},
		byProvider: map[string]ProviderConfig{
			"gateway": {ID: "gateway"},
		},
	}

	if err := registry.SetProviderKey("gateway", "test-key"); err != nil {
		t.Fatalf("set provider key: %v", err)
	}
	if key := registry.ProviderKey("gateway"); key != "test-key" {
		t.Fatalf("provider key = %q, want test-key", key)
	}

	payload, err := os.ReadFile(secretsPath)
	if err != nil {
		t.Fatalf("read secrets file: %v", err)
	}
	var saved secretsFile
	if err := json.Unmarshal(payload, &saved); err != nil {
		t.Fatalf("decode secrets file: %v", err)
	}
	if saved.Keys["gateway"] != "test-key" {
		t.Fatalf("saved provider key = %q, want test-key", saved.Keys["gateway"])
	}
}
