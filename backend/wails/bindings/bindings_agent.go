package bindings

import (
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

type AgentBindings struct{ host Host }

func (b *AgentBindings) GetAgent(sessionID, branchID, agentID string) (agentservice.AgentView, error) {
	if err := validateAgentQuery(sessionID, branchID, agentID); err != nil {
		return agentservice.AgentView{}, publicInputError(b.host, "AgentBindings.GetAgent.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return agentservice.AgentView{}, err
	}
	view, err := services.Agents.GetAgentView(ctx, identity.SessionID(sessionID), identity.AgentID(agentID), identity.BranchID(branchID))
	return view, publicError(b.host, "AgentBindings.GetAgent", err)
}

// ListAgentEntries 返回 Main 历史或 Subagent 当前执行私有消息的展示 DTO，不分页。
func (b *AgentBindings) ListAgentEntries(sessionID, branchID, agentID string) ([]dto.Entry, error) {
	if err := validateAgentQuery(sessionID, branchID, agentID); err != nil {
		return nil, publicInputError(b.host, "AgentBindings.ListAgentEntries.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	if _, err := services.Agents.GetAgentView(ctx, identity.SessionID(sessionID), identity.AgentID(agentID), identity.BranchID(branchID)); err != nil {
		return nil, publicError(b.host, "ListAgentEntries.scope", err)
	}
	entries, err := services.Commands.ListAgentEntries(ctx, identity.AgentID(agentID), identity.BranchID(branchID))
	if err != nil {
		return nil, publicError(b.host, "AgentBindings.ListAgentEntries", err)
	}
	return publicEntries(entries), nil
}

func validateAgentQuery(sessionID, branchID, agentID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return err
	}
	if err := validation.ID("branchId", branchID); err != nil {
		return err
	}
	return validation.ID("agentId", agentID)
}
