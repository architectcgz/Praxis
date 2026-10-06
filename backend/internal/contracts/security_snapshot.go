package contracts

import "strings"

// TaskRestrictions 描述一次执行请求可进一步收窄的权限范围。
type TaskRestrictions struct {
	AllowedTools        []ToolName
	EnforceAllowedTools bool
	ReadScopes          []string
	WriteScopes         []string
}

type SandboxConstraints struct {
	Mode SandboxMode
}

type ApprovalRule struct {
	Mode ApprovalMode
}

// ToolPermission 是一次 Task 冻结的工具启用状态与能力边界。
type ToolPermission struct {
	Enabled         bool `json:"enabled"`
	RequiresWrite   bool `json:"requiresWrite"`
	RequiresNetwork bool `json:"requiresNetwork"`
}

// SecuritySnapshot 是 task 创建时冻结的安全边界。
type SecuritySnapshot struct {
	AgentPolicyRevision uint64
	Permissions         TaskPermissions
	ToolPermissions     map[ToolName]ToolPermission
	Sandbox             SandboxConstraints
	ApprovalRules       []ApprovalRule
	Fingerprint         string
}

func (s SecuritySnapshot) Validate() error {
	if s.AgentPolicyRevision == 0 {
		return InvalidValue("taskSecuritySnapshot.agentPolicyRevision", "revision must be positive")
	}
	if err := s.Permissions.Validate(); err != nil {
		return FieldError("taskSecuritySnapshot.permissions", err)
	}
	for _, name := range s.Permissions.AllowedTools {
		if !s.ToolPermissions[name].Enabled {
			return InvalidValue("taskSecuritySnapshot.toolPermissions", "allowed tool needs an enabled permission")
		}
	}
	if !s.Sandbox.Mode.Valid() || len(s.ApprovalRules) == 0 {
		return InvalidValue("taskSecuritySnapshot", "sandbox and approval constraints are required")
	}
	if s.Fingerprint == "" || s.Fingerprint != strings.TrimSpace(s.Fingerprint) {
		return InvalidValue("taskSecuritySnapshot.fingerprint", "fingerprint is required")
	}
	for _, rule := range s.ApprovalRules {
		if !rule.Mode.Valid() {
			return InvalidValue("taskSecuritySnapshot.approvalRules", "unknown approval mode")
		}
	}
	return nil
}
