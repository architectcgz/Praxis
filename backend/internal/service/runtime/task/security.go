package task

import (
	"context"
	"praxis/internal/contracts"
	securitymodel "praxis/internal/core/security"
	taskmodel "praxis/internal/core/task"
)

// ResolveTaskSecurity 读取当前权限配置；Executor 再与 Task 冻结边界取交集。
func (s *Service) ResolveTaskSecurity(ctx context.Context, task taskmodel.Task) (contracts.SecuritySnapshot, error) {
	agent, err := s.agents.Get(ctx, task.AgentID)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	policy, err := s.policies.GetCurrent(ctx, agent.ID)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	session, err := s.sessions.Get(ctx, task.SessionID)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	workspace, err := s.workspaces.Get(ctx, session.WorkspaceID)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	definition, err := s.definitions.Definition(agent.DefinitionID)
	if err != nil {
		return contracts.SecuritySnapshot{}, err
	}
	s.securityMu.RLock()
	resolver := s.security
	s.securityMu.RUnlock()
	return resolver.Resolve(policy, contracts.TaskRestrictions{
		AllowedTools:        definition.AllowedTools,
		EnforceAllowedTools: true,
	}, workspace)
}

func systemSecurityBaseline(names []contracts.ToolName) (securitymodel.AgentSecurityPolicy, error) {
	return securitymodel.NewAgentSecurityPolicy(1, securitymodel.CapabilityPolicy{
		AllowedTools: names,
	}, securitymodel.SandboxPolicy{Mode: contracts.SandboxWorkspaceNetwork}, securitymodel.ApprovalPolicy{Mode: contracts.ApprovalYolo})
}
