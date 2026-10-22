package bindings

import (
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

type AgentBindings struct{ host Host }

func (b *AgentBindings) GetAgent(agentID string) (dto.AgentSnapshot, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return dto.AgentSnapshot{}, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.AgentSnapshot{}, err
	}
	view, err := services.Agents.GetAgentView(ctx, identity.AgentID(agentID), 100)
	if err != nil {
		return dto.AgentSnapshot{}, publicError(b.host, "AgentBindings.GetAgent", err)
	}
	result := agentSnapshot(view.Agent)
	result.ExecutionIDs = make([]string, 0, len(view.Executions))
	result.ControlCommandIDs = make([]string, 0, len(view.Controls))
	for _, value := range view.Executions {
		result.ExecutionIDs = append(result.ExecutionIDs, value.ID.String())
		result.Executions = append(result.Executions, executionSnapshot(value))
	}
	for _, value := range view.Controls {
		result.ControlCommandIDs = append(result.ControlCommandIDs, value.ID.String())
	}
	return result, nil
}

// ListAgentEntries 直接返回已恢复的完整可见历史，不包装展示 DTO、不分页。
func (b *AgentBindings) ListAgentEntries(agentID string) ([]sessionentry.ContextEntry, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	entries, err := services.Agents.ListAgentEntries(ctx, identity.AgentID(agentID))
	if err != nil {
		return nil, publicError(b.host, "AgentBindings.ListAgentEntries", err)
	}
	return entries, nil
}

func agentSnapshot(value agentmodel.Agent) dto.AgentSnapshot {
	return dto.AgentSnapshot{
		ID:                 value.ID.String(),
		Name:               agentDisplayName(value.DefinitionID),
		SessionID:          value.SessionID.String(),
		DefinitionID:       value.DefinitionID.String(),
		Profile:            string(value.Profile),
		State:              string(value.State),
		CurrentExecutionID: value.CurrentExecutionID.String(),
		Executions:         []dto.ExecutionSnapshot{},
	}
}

func executionSnapshot(value agentmodel.Execution) dto.ExecutionSnapshot {
	return dto.ExecutionSnapshot{
		SourceTaskID:   value.SourceTaskID.String(),
		ID:             value.ID.String(),
		Status:         string(value.Status),
		Outcome:        string(value.Outcome),
		FailureCode:    string(value.FailureCode),
		FailureMessage: value.FailureMessage,
		CreatedAt:      value.CreatedAt,
		StartedAt:      value.StartedAt,
		EndedAt:        value.EndedAt,
	}
}

// agentDisplayName 返回 Agent definition 对用户展示的英文名称，definition ID 只用于内部关联。
func agentDisplayName(id identity.AgentDefinitionID) string {
	switch id {
	case agentmodel.DefinitionPrimary:
		return "Leader"
	case agentmodel.DefinitionDelegate:
		return "Builder"
	case agentmodel.DefinitionAdvisor:
		return "Scout"
	case agentmodel.DefinitionCurator:
		return "Editor"
	default:
		return "Agent"
	}
}
