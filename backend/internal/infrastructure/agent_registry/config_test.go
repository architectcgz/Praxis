package agentregistry

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	domainsecurity "praxis/internal/domain/security"
	domainworkspace "praxis/internal/domain/workspace"
)

func TestLoadCreatesDefaultAgentsConfig(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	registry, err := Load(path, func(string, string) error { return nil })
	if err != nil {
		t.Fatalf("load default agents config: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("agents config was not created: %v", err)
	}
	if _, err := registry.SecurityPolicy(testWorkspace(), domainsecurity.ProfilePrimary); err != nil {
		t.Fatalf("resolve default primary policy: %v", err)
	}
	if _, err := registry.ResolveModel(domainsecurity.ProfilePrimary); err == nil {
		t.Fatal("expected primary model to remain unconfigured by default")
	}
}

func TestLoadResolvesAgentModelAndPolicy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	payload := FileConfig{
		Agents: map[domainsecurity.AgentProfile]AgentConfig{
			domainsecurity.ProfilePrimary: {
				Model: &ModelReference{ProviderID: "gateway", ModelID: "gpt"},
				Policy: PolicyConfig{
					ApprovalMode:      domainsecurity.ApprovalAlwaysAsk,
					SandboxMode:       domainsecurity.SandboxReadOnly,
					AllowedTools:      []domainsecurity.ToolName{domainsecurity.ToolReadFile},
					WorkspaceAccess:   domainsecurity.WorkspaceAccessRead,
					ResultPermissions: nil,
				},
			},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("encode agents config: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o600); err != nil {
		t.Fatalf("write agents config: %v", err)
	}
	registry, err := Load(path, func(providerID, modelID string) error {
		if providerID != "gateway" || modelID != "gpt" {
			t.Fatalf("validate model reference = %s/%s", providerID, modelID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("load agents config: %v", err)
	}
	selection, err := registry.ResolveModel(domainsecurity.ProfilePrimary)
	if err != nil {
		t.Fatalf("resolve primary model: %v", err)
	}
	if selection.ProviderID != "gateway" || selection.ModelID != "gpt" {
		t.Fatalf("selection = %#v", selection)
	}
	policy, err := registry.SecurityPolicy(testWorkspace(), domainsecurity.ProfilePrimary)
	if err != nil {
		t.Fatalf("resolve primary policy: %v", err)
	}
	if !policy.Capabilities.AllowedTools[0].Valid() || policy.Capabilities.ReadScopes[0] != testWorkspace().Path {
		t.Fatalf("policy = %#v", policy)
	}
}

func TestValidateUsesDefaultPolicyWhenAgentOnlyConfiguresModel(t *testing.T) {
	config, err := Validate(FileConfig{
		Agents: map[domainsecurity.AgentProfile]AgentConfig{
			domainsecurity.ProfilePrimary: {
				Model: &ModelReference{ProviderID: "gateway", ModelID: "gpt"},
			},
		},
	})
	if err != nil {
		t.Fatalf("validate model-only agent config: %v", err)
	}
	if config.Agents[domainsecurity.ProfilePrimary].Policy.ApprovalMode != domainsecurity.ApprovalAlwaysAsk {
		t.Fatalf("approval mode = %q, want %q", config.Agents[domainsecurity.ProfilePrimary].Policy.ApprovalMode, domainsecurity.ApprovalAlwaysAsk)
	}
}

func TestLoadRejectsUnknownAgentModel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "agents.json")
	content := `{"agents":{"primary":{"model":{"providerId":"missing","modelId":"gpt"},"policy":{"approvalMode":"always_ask","sandboxMode":"read_only","allowedTools":[],"workspaceAccess":"none","resultPermissions":[]}}}}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write agents config: %v", err)
	}
	if _, err := Load(path, func(providerID, modelID string) error {
		return &modelNotFoundError{providerID: providerID, modelID: modelID}
	}); err == nil {
		t.Fatal("expected unknown agent model to fail")
	}
}

type modelNotFoundError struct {
	providerID string
	modelID    string
}

func (e *modelNotFoundError) Error() string {
	return "model " + e.modelID + " for provider " + e.providerID + " is not configured"
}

func testWorkspace() domainworkspace.Workspace {
	return domainworkspace.Workspace{Path: filepath.Join(string(filepath.Separator), "workspace")}
}
