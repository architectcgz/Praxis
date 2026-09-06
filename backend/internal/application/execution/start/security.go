package start

import (
	"errors"
	"path/filepath"
	domaincontext "praxis/internal/core/domain/context"
	domainfoundation "praxis/internal/core/domain/foundation"
	domainsecurity "praxis/internal/core/domain/security"
	domainworkspace "praxis/internal/core/domain/workspace"
	"strings"

	corecommand "praxis/internal/core/command"
	"praxis/internal/core/system"
)

// SecurityResolver combines system limits, the Agent policy ceiling, and
// execution restrictions into one immutable execution snapshot.
type SecurityResolver struct {
	Baseline domainsecurity.AgentSecurityPolicy
	ids      system.IDGenerator
}

func NewSecurityResolver(baseline domainsecurity.AgentSecurityPolicy, generators ...system.IDGenerator) (SecurityResolver, error) {
	if err := baseline.Validate(); err != nil {
		return SecurityResolver{}, err
	}
	var ids system.IDGenerator = system.SecureIDGenerator{}
	if len(generators) > 0 && generators[0] != nil {
		ids = generators[0]
	}
	return SecurityResolver{Baseline: baseline.Snapshot(), ids: ids}, nil
}

func (r SecurityResolver) Resolve(
	policy domainsecurity.AgentSecurityPolicy,
	restrictions domainsecurity.ExecutionRestrictions,
	workspace domainworkspace.Workspace,
	model domainsecurity.ModelSelection,
	manifest domaincontext.ContextManifest,
) (domainsecurity.ExecutionSecuritySnapshot, error) {
	ids := r.ids
	if ids == nil {
		ids = system.SecureIDGenerator{}
	}
	if err := policy.Validate(); err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	if err := workspace.Validate(); err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	if err := manifest.Validate(); err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	if policy.Revision == 0 {
		return domainsecurity.ExecutionSecuritySnapshot{}, errors.New("agent security policy revision is required")
	}
	tools := intersectTools(policy.Capabilities.AllowedTools, r.Baseline.Capabilities.AllowedTools)
	tools, err := restrictTools(tools, restrictions.AllowedTools)
	if err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	readScopes, err := restrictScopes(intersectScopes(policy.Capabilities.ReadScopes, r.Baseline.Capabilities.ReadScopes), restrictions.ReadScopes)
	if err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	writeScopes, err := restrictScopes(intersectScopes(policy.Capabilities.WriteScopes, r.Baseline.Capabilities.WriteScopes), restrictions.WriteScopes)
	if err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	sandbox := policy.Sandbox.Mode
	if sandboxRank(r.Baseline.Sandbox.Mode) < sandboxRank(sandbox) {
		sandbox = r.Baseline.Sandbox.Mode
	}
	approval := policy.Approval.Mode
	if approvalRank(r.Baseline.Approval.Mode) < approvalRank(approval) {
		approval = r.Baseline.Approval.Mode
	}
	grant, err := domainsecurity.NewCapabilityGrant(domainsecurity.CapabilityGrantSpec{
		ID: domainfoundation.CapabilityGrantID(ids.New("grant")), WorkspaceID: workspace.ID,
		WorkspacePathSnapshot: workspace.Path, WorkspaceRevision: workspace.Revision,
		AllowedTools: tools, AllowedExecutables: intersectStrings(policy.Capabilities.AllowedExecutables, r.Baseline.Capabilities.AllowedExecutables), ReadScopes: readScopes, WriteScopes: writeScopes,
		CanProposeDelegation: containsTool(tools, domainsecurity.ToolProposeDelegate),
		ResultPermissions:    resultPermissions(tools), Model: model,
		ContextManifestRef: manifest.ID, ApprovalSource: domainsecurity.ApprovalSourcePolicyDefault,
		ApprovalPolicyFingerprint: corecommand.ArgumentsDigest(policy),
	})
	if err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	snapshot := domainsecurity.ExecutionSecuritySnapshot{
		AgentPolicyRevision: policy.Revision, CapabilityGrant: grant,
		Sandbox:       domainsecurity.SandboxConstraints{Mode: sandbox},
		ApprovalRules: []domainsecurity.ApprovalRule{{Mode: approval}},
		Fingerprint: corecommand.ArgumentsDigest(struct {
			Policy uint64
			Grant  domainsecurity.CapabilityGrant
		}{policy.Revision, grant}),
	}
	if err := snapshot.Validate(); err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, err
	}
	return snapshot, nil
}

func restrictTools(allowed, requested []domainsecurity.ToolName) ([]domainsecurity.ToolName, error) {
	if len(requested) == 0 {
		return append([]domainsecurity.ToolName(nil), allowed...), nil
	}
	set := make(map[domainsecurity.ToolName]struct{}, len(allowed))
	for _, tool := range allowed {
		set[tool] = struct{}{}
	}
	result := make([]domainsecurity.ToolName, 0, len(requested))
	for _, tool := range requested {
		if _, ok := set[tool]; !ok {
			return nil, errors.New("execution restrictions exceed agent security policy")
		}
		result = append(result, tool)
	}
	return result, nil
}

func restrictScopes(allowed, requested []string) ([]string, error) {
	if len(requested) == 0 {
		return append([]string(nil), allowed...), nil
	}
	result := make([]string, 0, len(requested))
	for _, scope := range requested {
		clean := filepath.Clean(strings.TrimSpace(scope))
		matched := false
		for _, root := range allowed {
			if pathWithin(root, clean) {
				matched = true
				break
			}
		}
		if !matched {
			return nil, errors.New("execution restrictions exceed agent security policy")
		}
		result = append(result, clean)
	}
	return result, nil
}

func intersectTools(left, right []domainsecurity.ToolName) []domainsecurity.ToolName {
	set := make(map[domainsecurity.ToolName]struct{}, len(right))
	for _, value := range right {
		set[value] = struct{}{}
	}
	result := make([]domainsecurity.ToolName, 0, len(left))
	for _, value := range left {
		if _, ok := set[value]; ok {
			result = append(result, value)
		}
	}
	return result
}

func intersectScopes(left, right []string) []string {
	if len(right) == 0 {
		return append([]string(nil), left...)
	}
	result := make([]string, 0, len(left))
	seen := make(map[string]struct{})
	for _, value := range left {
		for _, root := range right {
			intersection := ""
			if pathWithin(value, root) {
				intersection = filepath.Clean(root)
			} else if pathWithin(root, value) {
				intersection = filepath.Clean(value)
			}
			if intersection != "" {
				if _, exists := seen[intersection]; !exists {
					seen[intersection] = struct{}{}
					result = append(result, intersection)
				}
			}
		}
	}
	return result
}

func intersectStrings(left, right []string) []string {
	if len(right) == 0 {
		return append([]string(nil), left...)
	}
	set := make(map[string]struct{}, len(right))
	for _, value := range right {
		set[value] = struct{}{}
	}
	result := make([]string, 0, len(left))
	for _, value := range left {
		if _, ok := set[value]; ok {
			result = append(result, value)
		}
	}
	return result
}

func pathWithin(root, path string) bool {
	rel, err := filepath.Rel(filepath.Clean(root), filepath.Clean(path))
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func systemSecurityBaseline() (domainsecurity.AgentSecurityPolicy, error) {
	return domainsecurity.NewAgentSecurityPolicy(1, domainsecurity.CapabilityPolicy{
		AllowedTools: []domainsecurity.ToolName{
			domainsecurity.ToolReadFile, domainsecurity.ToolListDir, domainsecurity.ToolSearchText,
			domainsecurity.ToolWriteFile, domainsecurity.ToolRunCommand, domainsecurity.ToolProposeDelegate,
			domainsecurity.ToolSubmitResult, domainsecurity.ToolSubmitBriefing,
		},
	}, domainsecurity.SandboxPolicy{Mode: domainsecurity.SandboxWorkspaceNetwork}, domainsecurity.ApprovalPolicy{Mode: domainsecurity.ApprovalYolo})
}

func resultPermissions(tools []domainsecurity.ToolName) []domainsecurity.ResultPermission {
	result := make([]domainsecurity.ResultPermission, 0, 2)
	if containsTool(tools, domainsecurity.ToolSubmitResult) {
		result = append(result, domainsecurity.ResultPermissionAgentResult)
	}
	if containsTool(tools, domainsecurity.ToolSubmitBriefing) {
		result = append(result, domainsecurity.ResultPermissionBriefing)
	}
	return result
}

func sandboxRank(mode domainsecurity.SandboxMode) int {
	switch mode {
	case domainsecurity.SandboxReadOnly:
		return 0
	case domainsecurity.SandboxWorkspaceWrite:
		return 1
	case domainsecurity.SandboxWorkspaceNetwork:
		return 2
	default:
		return -1
	}
}

func approvalRank(mode domainsecurity.ApprovalMode) int {
	switch mode {
	case domainsecurity.ApprovalAlwaysAsk:
		return 0
	case domainsecurity.ApprovalAutoApproveDefaults:
		return 1
	case domainsecurity.ApprovalYolo:
		return 2
	default:
		return -1
	}
}
