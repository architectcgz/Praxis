package start

import (
	"path/filepath"
	domaincontext "praxis/internal/domain/context"
	domainmodel "praxis/internal/domain/model"
	domainsecurity "praxis/internal/domain/security"
	domainworkspace "praxis/internal/domain/workspace"
	"testing"
	"time"
)

type fixedIDs struct{}

func (fixedIDs) New(prefix string) string { return prefix + "_fixed" }

func TestSecurityResolverAllowsWorkspaceRootAndFreezesPolicy(t *testing.T) {
	root := filepath.Clean(`C:\work\praxis`)
	workspace, err := domainworkspace.NewWorkspace("workspace_test", "project_test", domainworkspace.WorkspaceProjectRoot, root, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	policy, err := domainsecurity.NewAgentSecurityPolicy(3, domainsecurity.CapabilityPolicy{
		AllowedTools: []domainsecurity.ToolName{domainsecurity.ToolReadFile}, ReadScopes: []string{root},
	}, domainsecurity.SandboxPolicy{Mode: domainsecurity.SandboxReadOnly}, domainsecurity.ApprovalPolicy{Mode: domainsecurity.ApprovalAlwaysAsk})
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := systemSecurityBaseline()
	if err != nil {
		t.Fatal(err)
	}
	resolver, err := NewSecurityResolver(baseline, fixedIDs{})
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := domaincontext.NewContextManifest("manifest_test", "test context", nil, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := resolver.Resolve(policy, domainsecurity.ExecutionRestrictions{ReadScopes: []string{root}}, workspace,
		domainmodel.ModelSelection{ProviderID: "provider", ModelID: "model"}, manifest)
	if err != nil {
		t.Fatal(err)
	}
	policy.Capabilities.ReadScopes[0] = filepath.Join(root, "changed")
	if got := snapshot.CapabilityGrant.ReadScopes[0]; got != root {
		t.Fatalf("snapshot scope changed with policy: %s", got)
	}
	if snapshot.AgentPolicyRevision != 3 || snapshot.CapabilityGrant.ID != "grant_fixed" {
		t.Fatalf("unexpected snapshot identity: %+v", snapshot)
	}
}

func TestSecurityResolverRejectsWiderRestriction(t *testing.T) {
	root := filepath.Clean(`C:\work\praxis`)
	workspace, _ := domainworkspace.NewWorkspace("workspace_test", "project_test", domainworkspace.WorkspaceProjectRoot, root, time.Now())
	policy, _ := domainsecurity.NewAgentSecurityPolicy(1, domainsecurity.CapabilityPolicy{
		AllowedTools: []domainsecurity.ToolName{domainsecurity.ToolReadFile}, ReadScopes: []string{filepath.Join(root, "src")},
	}, domainsecurity.SandboxPolicy{Mode: domainsecurity.SandboxReadOnly}, domainsecurity.ApprovalPolicy{Mode: domainsecurity.ApprovalAlwaysAsk})
	baseline, _ := systemSecurityBaseline()
	resolver, _ := NewSecurityResolver(baseline, fixedIDs{})
	manifest, _ := domaincontext.NewContextManifest("manifest_test", "test context", nil, time.Now())
	_, err := resolver.Resolve(policy, domainsecurity.ExecutionRestrictions{ReadScopes: []string{root}}, workspace,
		domainmodel.ModelSelection{ProviderID: "provider", ModelID: "model"}, manifest)
	if err == nil {
		t.Fatal("expected wider read restriction to be rejected")
	}
}

func TestSecurityResolverUsesNarrowerBaselineScope(t *testing.T) {
	root := filepath.Clean(`C:\work\praxis`)
	source := filepath.Join(root, "src")
	workspace, _ := domainworkspace.NewWorkspace("workspace_test", "project_test", domainworkspace.WorkspaceProjectRoot, root, time.Now())
	policy, _ := domainsecurity.NewAgentSecurityPolicy(1, domainsecurity.CapabilityPolicy{
		AllowedTools: []domainsecurity.ToolName{domainsecurity.ToolReadFile}, ReadScopes: []string{root},
	}, domainsecurity.SandboxPolicy{Mode: domainsecurity.SandboxReadOnly}, domainsecurity.ApprovalPolicy{Mode: domainsecurity.ApprovalAlwaysAsk})
	baseline, _ := domainsecurity.NewAgentSecurityPolicy(1, domainsecurity.CapabilityPolicy{
		AllowedTools: []domainsecurity.ToolName{domainsecurity.ToolReadFile}, ReadScopes: []string{source},
	}, domainsecurity.SandboxPolicy{Mode: domainsecurity.SandboxReadOnly}, domainsecurity.ApprovalPolicy{Mode: domainsecurity.ApprovalAlwaysAsk})
	resolver, _ := NewSecurityResolver(baseline, fixedIDs{})
	manifest, _ := domaincontext.NewContextManifest("manifest_test", "test context", nil, time.Now())
	snapshot, err := resolver.Resolve(policy, domainsecurity.ExecutionRestrictions{}, workspace,
		domainmodel.ModelSelection{ProviderID: "provider", ModelID: "model"}, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.CapabilityGrant.ReadScopes; len(got) != 1 || got[0] != source {
		t.Fatalf("read scopes=%v want=[%s]", got, source)
	}
}
