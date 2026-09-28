package start

import (
	"praxis/internal/contracts"
	securitymodel "praxis/internal/security"
	workspacemodel "praxis/internal/workspace"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"slices"
	"strings"
)

// SecurityResolver combines system limits, the Agent policy ceiling, and
// execution restrictions into one immutable execution snapshot.
type SecurityResolver struct {
	Baseline        securitymodel.AgentSecurityPolicy
	ToolPermissions securitymodel.ToolPermissionPolicy
}

func NewSecurityResolver(
	baseline securitymodel.AgentSecurityPolicy,
	toolPolicies ...securitymodel.ToolPermissionPolicy,
) (SecurityResolver, error) {
	if err := baseline.Validate(); err != nil {
		return SecurityResolver{}, err
	}
	toolPolicy := securitymodel.ToolPermissionPolicy{}
	if len(toolPolicies) > 0 {
		toolPolicy = toolPolicies[0]
	}
	if toolPolicy.Empty() {
		var err error
		toolPolicy, err = securitymodel.NewToolPermissionPolicy(nil)
		if err != nil {
			return SecurityResolver{}, err
		}
	}
	return SecurityResolver{Baseline: baseline.Snapshot(), ToolPermissions: toolPolicy}, nil
}

func (r SecurityResolver) Resolve(
	policy securitymodel.AgentSecurityPolicy,
	restrictions contracts.ExecutionRestrictions,
	workspace workspacemodel.Workspace,
) (contracts.ExecutionSecuritySnapshot, error) {
	if err := policy.Validate(); err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	if err := workspace.Validate(); err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	if policy.Revision == 0 {
		return contracts.ExecutionSecuritySnapshot{}, errors.New("agent security policy revision is required")
	}
	tools := intersectTools(policy.Capabilities.AllowedTools, r.Baseline.Capabilities.AllowedTools)
	tools, err := restrictTools(tools, restrictions.AllowedTools, restrictions.EnforceAllowedTools)
	if err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	readScopes, err := restrictScopes(intersectScopes(policy.Capabilities.ReadScopes, r.Baseline.Capabilities.ReadScopes), restrictions.ReadScopes)
	if err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	writeScopes, err := restrictScopes(intersectScopes(policy.Capabilities.WriteScopes, r.Baseline.Capabilities.WriteScopes), restrictions.WriteScopes)
	if err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	if err := validateWorkspaceScopes(workspace.Path, readScopes); err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	if err := validateWorkspaceScopes(workspace.Path, writeScopes); err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	sandbox := policy.Sandbox.Mode
	if sandboxRank(r.Baseline.Sandbox.Mode) < sandboxRank(sandbox) {
		sandbox = r.Baseline.Sandbox.Mode
	}
	approval := policy.Approval.Mode
	if approvalRank(r.Baseline.Approval.Mode) < approvalRank(approval) {
		approval = r.Baseline.Approval.Mode
	}
	tools = r.ToolPermissions.FilterTools(tools, sandbox, len(writeScopes) > 0)
	executables := intersectStrings(policy.Capabilities.AllowedExecutables, r.Baseline.Capabilities.AllowedExecutables)
	if approval != contracts.ApprovalYolo || !sandbox.AllowsWorkspaceWrite() ||
		len(writeScopes) == 0 || !allowsBash(executables) {
		tools = slices.DeleteFunc(tools, func(name contracts.ToolName) bool { return name == contracts.ToolBash })
	}
	permissions, err := contracts.NewExecutionPermissions(contracts.ExecutionPermissionsSpec{
		WorkspaceID:        workspace.ID,
		WorkspaceRevision:  workspace.Revision,
		AllowedTools:       tools,
		AllowedExecutables: executables,
		ReadScopes:         readScopes,
		WriteScopes:        writeScopes,
	})
	if err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	snapshot := contracts.ExecutionSecuritySnapshot{
		AgentPolicyRevision: policy.Revision, Permissions: permissions,
		ToolPermissions: r.ToolPermissions.SnapshotFor(tools),
		Sandbox:         contracts.SandboxConstraints{Mode: sandbox},
		ApprovalRules:   []contracts.ApprovalRule{{Mode: approval}},
		Fingerprint: securityFingerprint(struct {
			Policy          uint64
			Permissions     contracts.ExecutionPermissions
			ToolPermissions map[contracts.ToolName]contracts.ToolPermission
			Sandbox         contracts.SandboxMode
		}{
			Policy:          policy.Revision,
			Permissions:     permissions,
			ToolPermissions: r.ToolPermissions.SnapshotFor(tools),
			Sandbox:         sandbox,
		}),
	}
	if err := snapshot.Validate(); err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	return snapshot, nil
}

// securityFingerprint 生成执行期间不可变安全快照的版本指纹。
func securityFingerprint(value any) string {
	encoded, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:])
}

func restrictTools(allowed, requested []contracts.ToolName, enforce bool) ([]contracts.ToolName, error) {
	if len(requested) == 0 && !enforce {
		return append([]contracts.ToolName(nil), allowed...), nil
	}
	set := make(map[contracts.ToolName]struct{}, len(allowed))
	for _, tool := range allowed {
		set[tool] = struct{}{}
	}
	result := make([]contracts.ToolName, 0, len(requested))
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

func intersectTools(left, right []contracts.ToolName) []contracts.ToolName {
	set := make(map[contracts.ToolName]struct{}, len(right))
	for _, value := range right {
		set[value] = struct{}{}
	}
	result := make([]contracts.ToolName, 0, len(left))
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

func systemSecurityBaseline(names []contracts.ToolName) (securitymodel.AgentSecurityPolicy, error) {
	return securitymodel.NewAgentSecurityPolicy(1, securitymodel.CapabilityPolicy{
		AllowedTools: names,
	}, securitymodel.SandboxPolicy{Mode: contracts.SandboxWorkspaceNetwork}, securitymodel.ApprovalPolicy{Mode: contracts.ApprovalYolo})
}

func sandboxRank(mode contracts.SandboxMode) int {
	switch mode {
	case contracts.SandboxReadOnly:
		return 0
	case contracts.SandboxWorkspaceWrite:
		return 1
	case contracts.SandboxWorkspaceNetwork:
		return 2
	default:
		return -1
	}
}

func approvalRank(mode contracts.ApprovalMode) int {
	switch mode {
	case contracts.ApprovalAlwaysAsk:
		return 0
	case contracts.ApprovalAutoApproveDefaults:
		return 1
	case contracts.ApprovalYolo:
		return 2
	default:
		return -1
	}
}

func validateWorkspaceScopes(workspacePath string, scopes []string) error {
	for _, scope := range scopes {
		if !pathWithin(workspacePath, scope) {
			return errors.New("execution permission scope must remain inside the workspace")
		}
	}
	return nil
}

func allowsBash(executables []string) bool {
	for _, executable := range executables {
		name := strings.ToLower(filepath.Base(executable))
		if name == "bash" || name == "bash.exe" {
			return true
		}
	}
	return false
}
