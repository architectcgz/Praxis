package security

import (
	"strings"

	"praxis/internal/contracts"
)

type CapabilityPolicy struct {
	AllowedTools       []contracts.ToolName
	ReadScopes         []string
	WriteScopes        []string
	AllowedExecutables []string
}

type SandboxPolicy struct {
	Mode contracts.SandboxMode
}

type ApprovalPolicy struct {
	Mode contracts.ApprovalMode
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
		return contracts.InvalidValue("agentSecurityPolicy.revision", "revision must be positive")
	}
	if !p.Sandbox.Mode.Valid() || !p.Approval.Mode.Valid() {
		return contracts.InvalidValue("agentSecurityPolicy", "unknown sandbox or approval mode")
	}
	seen := make(map[contracts.ToolName]struct{}, len(p.Capabilities.AllowedTools))
	for _, tool := range p.Capabilities.AllowedTools {
		if !tool.Valid() {
			return contracts.InvalidValue("agentSecurityPolicy.capabilities.allowedTools", "unknown tool")
		}
		if _, ok := seen[tool]; ok {
			return contracts.InvalidValue("agentSecurityPolicy.capabilities.allowedTools", "duplicate tool")
		}
		seen[tool] = struct{}{}
	}
	for _, scope := range append(append([]string{}, p.Capabilities.ReadScopes...), p.Capabilities.WriteScopes...) {
		if strings.TrimSpace(scope) == "" || strings.ContainsAny(scope, "\x00\r\n") {
			return contracts.InvalidValue("agentSecurityPolicy.capabilities.scopes", "scope is invalid")
		}
	}
	return nil
}

func (p AgentSecurityPolicy) Snapshot() AgentSecurityPolicy {
	return AgentSecurityPolicy{Revision: p.Revision, Capabilities: cloneCapabilityPolicy(p.Capabilities), Sandbox: p.Sandbox, Approval: p.Approval}
}

func cloneCapabilityPolicy(value CapabilityPolicy) CapabilityPolicy {
	return CapabilityPolicy{
		AllowedTools:       append([]contracts.ToolName(nil), value.AllowedTools...),
		ReadScopes:         append([]string(nil), value.ReadScopes...),
		WriteScopes:        append([]string(nil), value.WriteScopes...),
		AllowedExecutables: append([]string(nil), value.AllowedExecutables...),
	}
}
