package security

import "strings"

type CapabilityPolicy struct {
	AllowedTools       []ToolName
	ReadScopes         []string
	WriteScopes        []string
	AllowedExecutables []string
}

type SandboxPolicy struct {
	Mode SandboxMode
}

type ApprovalPolicy struct {
	Mode ApprovalMode
}

// AgentSecurityPolicy is the mutable permission ceiling owned by one Agent.
type AgentSecurityPolicy struct {
	Revision     uint64
	Capabilities CapabilityPolicy
	Sandbox      SandboxPolicy
	Approval     ApprovalPolicy
}

func NewAgentSecurityPolicy(revision uint64, capabilities CapabilityPolicy, sandbox SandboxPolicy, approval ApprovalPolicy) (AgentSecurityPolicy, error) {
	policy := AgentSecurityPolicy{Revision: revision, Capabilities: cloneCapabilityPolicy(capabilities), Sandbox: sandbox, Approval: approval}
	if err := policy.Validate(); err != nil {
		return AgentSecurityPolicy{}, err
	}
	return policy, nil
}

func (p AgentSecurityPolicy) Validate() error {
	if p.Revision == 0 {
		return invalidValue("agentSecurityPolicy.revision", "revision must be positive")
	}
	if !p.Sandbox.Mode.Valid() || !p.Approval.Mode.Valid() {
		return invalidValue("agentSecurityPolicy", "unknown sandbox or approval mode")
	}
	seen := make(map[ToolName]struct{}, len(p.Capabilities.AllowedTools))
	for _, tool := range p.Capabilities.AllowedTools {
		if !tool.Valid() {
			return invalidValue("agentSecurityPolicy.capabilities.allowedTools", "unknown tool")
		}
		if _, ok := seen[tool]; ok {
			return invalidValue("agentSecurityPolicy.capabilities.allowedTools", "duplicate tool")
		}
		seen[tool] = struct{}{}
	}
	for _, scope := range append(append([]string{}, p.Capabilities.ReadScopes...), p.Capabilities.WriteScopes...) {
		if strings.TrimSpace(scope) == "" || strings.ContainsAny(scope, "\x00\r\n") {
			return invalidValue("agentSecurityPolicy.capabilities.scopes", "scope is invalid")
		}
	}
	return nil
}

func (p AgentSecurityPolicy) Snapshot() AgentSecurityPolicy {
	return AgentSecurityPolicy{Revision: p.Revision, Capabilities: cloneCapabilityPolicy(p.Capabilities), Sandbox: p.Sandbox, Approval: p.Approval}
}

type ExecutionRestrictions struct {
	AllowedTools []ToolName
	ReadScopes   []string
	WriteScopes  []string
}

type SandboxConstraints struct {
	Mode SandboxMode
}

type ApprovalRule struct {
	Mode ApprovalMode
}

type ExecutionSecuritySnapshot struct {
	AgentPolicyRevision uint64
	CapabilityGrant     CapabilityGrant
	Sandbox             SandboxConstraints
	ApprovalRules       []ApprovalRule
	Fingerprint         string
}

func (s ExecutionSecuritySnapshot) Validate() error {
	if s.AgentPolicyRevision == 0 {
		return invalidValue("executionSecuritySnapshot.agentPolicyRevision", "revision must be positive")
	}
	if err := s.CapabilityGrant.Validate(); err != nil {
		return fmtField("executionSecuritySnapshot.capabilityGrant", err)
	}
	if !s.Sandbox.Mode.Valid() || len(s.ApprovalRules) == 0 {
		return invalidValue("executionSecuritySnapshot", "sandbox and approval constraints are required")
	}
	if strings.TrimSpace(s.Fingerprint) == "" {
		return invalidValue("executionSecuritySnapshot.fingerprint", "fingerprint is required")
	}
	for _, rule := range s.ApprovalRules {
		if !rule.Mode.Valid() {
			return invalidValue("executionSecuritySnapshot.approvalRules", "unknown approval mode")
		}
	}
	return nil
}

func (s ExecutionSecuritySnapshot) Snapshot() ExecutionSecuritySnapshot {
	copy := s
	copy.CapabilityGrant = s.CapabilityGrant.Snapshot()
	copy.ApprovalRules = append([]ApprovalRule(nil), s.ApprovalRules...)
	return copy
}

func cloneCapabilityPolicy(value CapabilityPolicy) CapabilityPolicy {
	return CapabilityPolicy{
		AllowedTools:       append([]ToolName(nil), value.AllowedTools...),
		ReadScopes:         append([]string(nil), value.ReadScopes...),
		WriteScopes:        append([]string(nil), value.WriteScopes...),
		AllowedExecutables: append([]string(nil), value.AllowedExecutables...),
	}
}
