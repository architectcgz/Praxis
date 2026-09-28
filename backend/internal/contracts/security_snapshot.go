package contracts

import "strings"

// ExecutionRestrictions 描述一次执行请求可进一步收窄的权限范围。
type ExecutionRestrictions struct {
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

// ToolPermission 是一次 Execution 冻结的工具启用状态与能力边界。
type ToolPermission struct {
	Enabled         bool `json:"enabled"`
	RequiresWrite   bool `json:"requiresWrite"`
	RequiresNetwork bool `json:"requiresNetwork"`
}

// ExecutionSecuritySnapshot 是 execution 创建时冻结的安全边界。
type ExecutionSecuritySnapshot struct {
	AgentPolicyRevision uint64
	Permissions         ExecutionPermissions
	ToolPermissions     map[ToolName]ToolPermission
	Sandbox             SandboxConstraints
	ApprovalRules       []ApprovalRule
	Fingerprint         string
}

func (s ExecutionSecuritySnapshot) Validate() error {
	if s.AgentPolicyRevision == 0 {
		return InvalidValue("executionSecuritySnapshot.agentPolicyRevision", "revision must be positive")
	}
	if err := s.Permissions.Validate(); err != nil {
		return FieldError("executionSecuritySnapshot.permissions", err)
	}
	for _, name := range s.Permissions.AllowedTools {
		if !s.ToolPermissions[name].Enabled {
			return InvalidValue("executionSecuritySnapshot.toolPermissions", "allowed tool needs an enabled permission")
		}
	}
	if !s.Sandbox.Mode.Valid() || len(s.ApprovalRules) == 0 {
		return InvalidValue("executionSecuritySnapshot", "sandbox and approval constraints are required")
	}
	if strings.TrimSpace(s.Fingerprint) == "" {
		return InvalidValue("executionSecuritySnapshot.fingerprint", "fingerprint is required")
	}
	for _, rule := range s.ApprovalRules {
		if !rule.Mode.Valid() {
			return InvalidValue("executionSecuritySnapshot.approvalRules", "unknown approval mode")
		}
	}
	return nil
}

func (s ExecutionSecuritySnapshot) Snapshot() ExecutionSecuritySnapshot {
	copy := s
	copy.Permissions = s.Permissions.Snapshot()
	copy.ToolPermissions = make(map[ToolName]ToolPermission, len(s.ToolPermissions))
	for name, permission := range s.ToolPermissions {
		copy.ToolPermissions[name] = permission
	}
	copy.ApprovalRules = append([]ApprovalRule(nil), s.ApprovalRules...)
	return copy
}
