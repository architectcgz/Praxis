package dataroot

import (
	"path/filepath"
	"testing"
)

func TestResolveUsesConfigForModelProviders(t *testing.T) {
	root := filepath.Join(t.TempDir(), "praxis")

	resolved, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := filepath.Join(root, "config", "models.json")
	if resolved.ModelProvidersConfig != want {
		t.Fatalf("ModelProvidersConfig = %q, want %q", resolved.ModelProvidersConfig, want)
	}
}
