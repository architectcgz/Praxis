package bindings

import (
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

type AgentBindings struct{ host Host }

func (b *AgentBindings) GetAgent(agentID string) (agentservice.AgentView, error) {
	if err := validation.ID("agentId", agentID); err != nil {
		return agentservice.AgentView{}, publicInputError(b.host, "AgentBindings.GetAgent.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return agentservice.AgentView{}, err
	}
	view, err := services.Agents.GetAgentView(ctx, identity.AgentID(agentID))
	return view, publicError(b.host, "AgentBindings.GetAgent", err)
}

// ListAgentEntries 返回 Main 历史或 Subagent 当前执行私有消息的展示 DTO，不分页。
func (b *AgentBindings) ListAgentEntries(agentID string) ([]dto.Entry, error) {
	if err := validation.ID("agentId", agentID); err != nil {
		return nil, publicInputError(b.host, "AgentBindings.ListAgentEntries.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	entries, err := services.Commands.ListAgentEntries(ctx, identity.AgentID(agentID))
	if err != nil {
		return nil, publicError(b.host, "AgentBindings.ListAgentEntries", err)
	}
	return publicEntries(entries), nil
}
