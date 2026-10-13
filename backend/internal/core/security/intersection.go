package security

import (
	"praxis/internal/contracts"
	"praxis/internal/utils/pathutil"
	"slices"
)

// IntersectSnapshot 取冻结权限与当前强制约束的交集，权限撤销立即影响后续准入。
func IntersectSnapshot(frozen, current contracts.SecuritySnapshot) contracts.SecuritySnapshot {
	result := frozen
	result.Permissions = frozen.Permissions
	result.Permissions.AllowedTools = nil
	result.ToolPermissions = make(map[contracts.ToolName]contracts.ToolPermission)
	for _, name := range frozen.Permissions.AllowedTools {
		old, now := frozen.ToolPermissions[name], current.ToolPermissions[name]
		if old.Enabled && now.Enabled && current.Permissions.AllowsTool(name) {
			result.Permissions.AllowedTools = append(result.Permissions.AllowedTools, name)
			result.ToolPermissions[name] = contracts.ToolPermission{Enabled: true, RequiresWrite: old.RequiresWrite || now.RequiresWrite, RequiresNetwork: old.RequiresNetwork || now.RequiresNetwork}
		}
	}
	result.Permissions.ReadScopes = intersectPaths(frozen.Permissions.ReadScopes, current.Permissions.ReadScopes)
	result.Permissions.WriteScopes = intersectPaths(frozen.Permissions.WriteScopes, current.Permissions.WriteScopes)
	result.Permissions.AllowedExecutables = nil
	for _, executable := range frozen.Permissions.AllowedExecutables {
		if slices.Contains(current.Permissions.AllowedExecutables, executable) {
			result.Permissions.AllowedExecutables = append(result.Permissions.AllowedExecutables, executable)
		}
	}
	if !current.Sandbox.Mode.AllowsWorkspaceWrite() {
		result.Permissions.WriteScopes = nil
	}
	if !current.Sandbox.Mode.AllowsNetwork() {
		for name, permission := range result.ToolPermissions {
			if permission.RequiresNetwork {
				delete(result.ToolPermissions, name)
			}
		}
	}
	if approvalPriority(current.ApprovalRules[0].Mode) < approvalPriority(frozen.ApprovalRules[0].Mode) {
		result.ApprovalRules = slices.Clone(current.ApprovalRules)
	}
	return result
}
func intersectPaths(left, right []string) []string {
	result := make([]string, 0)
	for _, a := range left {
		for _, b := range right {
			path := ""
			if pathutil.IsWithin(a, b) {
				path = b
			} else if pathutil.IsWithin(b, a) {
				path = a
			}
			if path != "" && !slices.Contains(result, path) {
				result = append(result, path)
			}
		}
	}
	return result
}
func approvalPriority(mode contracts.ApprovalMode) int {
	switch mode {
	case contracts.ApprovalAlwaysAsk:
		return 0
	case contracts.ApprovalAutoApproveDefaults:
		return 1
	default:
		return 2
	}
}

// RequiresToolApproval 只决定已允许的调用是否需要用户确认。
func RequiresToolApproval(snapshot contracts.SecuritySnapshot, name contracts.ToolName) bool {
	switch snapshot.ApprovalRules[0].Mode {
	case contracts.ApprovalAlwaysAsk:
		return true
	case contracts.ApprovalAutoApproveDefaults:
		return toolRequiresWrite(name, snapshot.ToolPermissions[name])
	default:
		return false
	}
}
