package security

import (
	"praxis/internal/contracts"
	workspacemodel "praxis/internal/core/workspace"
	"praxis/internal/utils/pathutil"

	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
)

// SecurityResolver 将系统策略、Agent 策略和 Task 限制收敛为不可变安全快照。
type SecurityResolver struct {
	Baseline        AgentSecurityPolicy
	ToolPermissions ToolPermissionPolicy
}

// NewSecurityResolver 复制已校验的系统权限上限；未指定工具策略时使用默认策略。
func NewSecurityResolver(
	baseline AgentSecurityPolicy,
	toolPolicies ...ToolPermissionPolicy,
) (SecurityResolver, error) {
	if err := baseline.Validate(); err != nil {
		return SecurityResolver{}, err
	}
	toolPolicy := ToolPermissionPolicy{}
	if len(toolPolicies) > 0 {
		toolPolicy = toolPolicies[0]
	}
	if toolPolicy.Empty() {
		var err error
		toolPolicy, err = NewToolPermissionPolicy(nil)
		if err != nil {
			return SecurityResolver{}, err
		}
	}
	return SecurityResolver{Baseline: baseline.Snapshot(), ToolPermissions: toolPolicy}, nil
}

// Resolve 只校验并收窄 canonical 权限，不清洗被授权路径；越权或无效输入返回错误。
func (r SecurityResolver) Resolve(
	policy AgentSecurityPolicy,
	restrictions contracts.TaskRestrictions,
	workspace workspacemodel.Workspace,
) (contracts.SecuritySnapshot, error) {
	if err := policy.Validate(); err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	if err := workspace.Validate(); err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	if policy.Revision == 0 {
		return contracts.SecuritySnapshot{}, errors.New("agent security policy revision is required")
	}
	tools, err := restrictTools(
		policy.Capabilities.AllowedTools, r.Baseline.Capabilities.AllowedTools,
		restrictions.AllowedTools, restrictions.EnforceAllowedTools,
	)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	readScopes, err := restrictScopes(intersectScopes(policy.Capabilities.ReadScopes, r.Baseline.Capabilities.ReadScopes), restrictions.ReadScopes, "读取")
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	writeScopes, err := restrictScopes(intersectScopes(policy.Capabilities.WriteScopes, r.Baseline.Capabilities.WriteScopes), restrictions.WriteScopes, "写入")
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	if err := validateWorkspaceScopes(workspace.Path, readScopes); err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	if err := validateWorkspaceScopes(workspace.Path, writeScopes); err != nil {
		return contracts.SecuritySnapshot{}, err
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
	if !sandbox.AllowsWorkspaceWrite() ||
		len(writeScopes) == 0 || !allowsBash(executables) {
		tools = slices.DeleteFunc(tools, func(name contracts.ToolName) bool { return name == contracts.ToolBash })
	}
	permissions := contracts.TaskPermissions{
		WorkspaceID:        workspace.ID,
		WorkspaceRevision:  workspace.Revision,
		AllowedTools:       slices.Clone(tools),
		AllowedExecutables: slices.Clone(executables),
		// 空范围用空数组表示，保持快照序列化与指纹的确定性。
		ReadScopes:  append([]string{}, readScopes...),
		WriteScopes: append([]string{}, writeScopes...),
	}
	// 排序只稳定快照指纹，不替输入边界修正被授权路径或可执行文件。
	slices.Sort(permissions.AllowedTools)
	slices.Sort(permissions.AllowedExecutables)
	slices.Sort(permissions.ReadScopes)
	slices.Sort(permissions.WriteScopes)
	snapshot := contracts.SecuritySnapshot{
		AgentPolicyRevision: policy.Revision,
		Permissions:         permissions,
		ToolPermissions:     r.ToolPermissions.SnapshotFor(tools),
		Sandbox:             contracts.SandboxConstraints{Mode: sandbox},
		ApprovalRules:       []contracts.ApprovalRule{{Mode: approval}},
		Fingerprint: securityFingerprint(struct {
			Policy          uint64
			Permissions     contracts.TaskPermissions
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
		return contracts.SecuritySnapshot{}, err
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

func restrictTools(policyTools, systemTools, requested []contracts.ToolName, enforce bool) ([]contracts.ToolName, error) {
	allowed := intersectTools(policyTools, systemTools)
	if len(requested) == 0 && !enforce {
		return allowed, nil
	}
	set := make(map[contracts.ToolName]struct{}, len(allowed))
	for _, tool := range allowed {
		set[tool] = struct{}{}
	}
	result := make([]contracts.ToolName, 0, len(requested))
	for _, tool := range requested {
		if _, ok := set[tool]; !ok {
			// 先区分系统支持范围与会话授权，避免建议用户为已移除的工具扩大权限。
			if !slices.Contains(systemTools, tool) {
				return nil, contracts.New(contracts.TaskPolicyBlocked, fmt.Sprintf(
					"无法开始执行：工具 %q 不在当前系统可用工具列表中（%v）。请从配置文件的 policy.allowedTools 中移除 %q，保存并重启 Praxis，然后在当前会话重试。",
					tool, systemTools, tool,
				))
			}
			return nil, contracts.New(contracts.TaskPolicyBlocked, fmt.Sprintf(
				"无法开始执行：当前会话未授权工具 %q（已授权工具：%v）。请从配置文件的 policy.allowedTools 中移除 %q 后重启并重试；如果确实需要该工具，请确认配置中的授权后新建会话。已有会话不会自动获得新增权限。",
				tool, policyTools, tool,
			))
		}
		result = append(result, tool)
	}
	return result, nil
}

func restrictScopes(allowed, requested []string, access string) ([]string, error) {
	if len(requested) == 0 {
		return slices.Clone(allowed), nil
	}
	result := make([]string, 0, len(requested))
	for _, scope := range requested {
		if !pathutil.IsAbsoluteNormalized(scope) {
			return nil, contracts.InvalidValue("taskRestrictions.scopes", "scope must be an absolute normalized path")
		}
		matched := false
		for _, root := range allowed {
			if pathutil.IsWithin(root, scope) {
				matched = true
				break
			}
		}
		if !matched {
			return nil, contracts.New(contracts.TaskPolicyBlocked, fmt.Sprintf(
				"无法开始执行：请求的%s路径 %q 超出当前授权范围 %v。请改用已授权目录内的路径；若需要操作其他目录，请将该目录作为项目打开并新建会话。",
				access, scope, allowed,
			))
		}
		result = append(result, scope)
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
		return slices.Clone(left)
	}
	return intersectPaths(left, right)
}

func intersectStrings(left, right []string) []string {
	if len(right) == 0 {
		return slices.Clone(left)
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
		if !pathutil.IsWithin(workspacePath, scope) {
			return contracts.New(contracts.TaskPolicyBlocked, fmt.Sprintf(
				"无法开始执行：授权路径 %q 不在当前工作区 %q 内。请使用工作区内的路径；如果项目目录已更改，请在目标项目中新建会话。",
				scope, workspacePath,
			))
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
