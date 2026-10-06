package security

import (
	"fmt"

	"praxis/internal/contracts"
)

// ToolPermissionPolicy 是进程级不可变 Tool 权限策略。
type ToolPermissionPolicy struct {
	tools map[contracts.ToolName]contracts.ToolPermission
}

// NewToolPermissionPolicy 创建策略。未列出的扩展 Tool 默认拒绝。
func NewToolPermissionPolicy(overrides map[contracts.ToolName]contracts.ToolPermission) (ToolPermissionPolicy, error) {
	tools := map[contracts.ToolName]contracts.ToolPermission{
		contracts.ToolReadFile:   {Enabled: true},
		contracts.ToolBash:       {Enabled: true, RequiresWrite: true},
		contracts.ToolApplyPatch: {Enabled: true, RequiresWrite: true},
	}
	for name, permission := range overrides {
		if !name.Valid() {
			return ToolPermissionPolicy{}, fmt.Errorf("invalid tool permission name %q", name)
		}
		tools[name] = permission
	}
	return ToolPermissionPolicy{tools: tools}, nil
}

// Empty 报告策略是否尚未初始化。
func (p ToolPermissionPolicy) Empty() bool { return len(p.tools) == 0 }

// ValidateRegistered 拒绝配置中没有对应代码实现的 Tool。
func (p ToolPermissionPolicy) ValidateRegistered(names []contracts.ToolName) error {
	registered := make(map[contracts.ToolName]struct{}, len(names))
	for _, name := range names {
		registered[name] = struct{}{}
	}
	for name := range p.tools {
		if _, ok := registered[name]; !ok {
			return fmt.Errorf("tool permission is configured for unregistered tool %q", name)
		}
	}
	return nil
}

// SnapshotFor 冻结有效 Tool 的能力要求，供执行准入使用。
func (p ToolPermissionPolicy) SnapshotFor(names []contracts.ToolName) map[contracts.ToolName]contracts.ToolPermission {
	result := make(map[contracts.ToolName]contracts.ToolPermission, len(names))
	for _, name := range names {
		if permission, ok := p.tools[name]; ok && permission.Enabled {
			result[name] = permission
		}
	}
	return result
}

// FilterTools 根据全局策略和 Task 能力筛选模型可见的 Tool 集合。
func (p ToolPermissionPolicy) FilterTools(
	names []contracts.ToolName,
	sandbox contracts.SandboxMode,
	hasWriteAccess bool,
) []contracts.ToolName {
	result := make([]contracts.ToolName, 0, len(names))
	for _, name := range names {
		permission, ok := p.tools[name]
		if ok && permission.Enabled &&
			(!toolRequiresWrite(name, permission) || sandbox.AllowsWorkspaceWrite() && hasWriteAccess) &&
			(!permission.RequiresNetwork || sandbox.AllowsNetwork()) {
			result = append(result, name)
		}
	}
	return result
}

// Allows 判断一次已经归一化的调用是否满足全局权限策略和 Task 边界。
func AllowsToolCall(
	snapshot contracts.SecuritySnapshot,
	name contracts.ToolName,
	path string,
) bool {
	permission, ok := snapshot.ToolPermissions[name]
	permissions := snapshot.Permissions
	sandbox := snapshot.Sandbox.Mode
	if !ok || !permission.Enabled || !permissions.AllowsTool(name) {
		return false
	}
	if name == contracts.ToolBash &&
		(len(snapshot.ApprovalRules) == 0 || snapshot.ApprovalRules[0].Mode != contracts.ApprovalYolo ||
			!sandbox.AllowsWorkspaceWrite() || !permissions.HasWriteAccess() ||
			len(permissions.AllowedExecutables) == 0) {
		return false
	}
	if toolRequiresWrite(name, permission) {
		if !sandbox.AllowsWorkspaceWrite() || !permissions.HasWriteAccess() {
			return false
		}
		if path != "" && !permissions.AllowsWritePath(path) {
			return false
		}
	} else if path != "" && !permissions.AllowsReadPath(path) {
		return false
	}
	return !permission.RequiresNetwork || sandbox.AllowsNetwork()
}

func toolRequiresWrite(name contracts.ToolName, permission contracts.ToolPermission) bool {
	return permission.RequiresWrite || name == contracts.ToolApplyPatch
}
