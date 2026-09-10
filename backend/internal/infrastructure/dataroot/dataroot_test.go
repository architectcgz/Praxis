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

func TestResolveUsesConfigForAgents(t *testing.T) {
	root := filepath.Join(t.TempDir(), "praxis")

	resolved, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := filepath.Join(root, "config", "agents.json")
	if resolved.AgentConfigFile != want {
		t.Fatalf("AgentConfigFile = %q, want %q", resolved.AgentConfigFile, want)
	}
}

func TestResolveUsesConfigForModelCredentials(t *testing.T) {
	root := filepath.Join(t.TempDir(), "praxis")

	resolved, err := Resolve(root)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}

	want := filepath.Join(root, "config", "auth.json")
	if resolved.ModelCredentialsFile != want {
		t.Fatalf("ModelCredentialsFile = %q, want %q", resolved.ModelCredentialsFile, want)
	}
}
