package bindings

import (
	"praxis/internal/contracts"
	"praxis/internal/service"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

type AgentBindings struct {
	runtime Runtime
}

func (b *AgentBindings) GetAgent(agentID string) (dto.AgentSnapshot, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return dto.AgentSnapshot{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.AgentSnapshot{}, err
	}
	value, err := services.Agents.GetAgentDetail(ctx, contracts.AgentID(agentID), 100)
	if err != nil {
		return dto.AgentSnapshot{}, publicError(b.runtime, "AgentBindings.GetAgent", err)
	}
	return agentSnapshot(value), nil
}

func (b *AgentBindings) ListAgentMessages(agentID string) ([]dto.AgentMessage, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	messages, err := services.Agents.ListAgentMessages(ctx, contracts.AgentID(agentID), 200)
	if err != nil {
		return nil, publicError(b.runtime, "AgentBindings.ListAgentMessages", err)
	}
	result := make([]dto.AgentMessage, 0, len(messages))
	for _, message := range messages {
		result = append(result, agentMessage(message))
	}
	return result, nil
}

func (b *AgentBindings) ListAgentHistory(agentID string) ([]dto.AgentHistoryItem, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	items, err := services.Agents.ListAgentHistory(ctx, contracts.AgentID(agentID), 200)
	if err != nil {
		return nil, publicError(b.runtime, "AgentBindings.ListAgentHistory", err)
	}
	result := make([]dto.AgentHistoryItem, 0, len(items))
	for _, item := range items {
		value := dto.AgentHistoryItem{
			Kind:     item.Kind,
			Sequence: item.Sequence,
			At:       item.At,
		}
		if item.Message != nil {
			message := agentMessage(*item.Message)
			value.Message = &message
		}
		if item.Task != nil {
			task := taskSnapshot(*item.Task)
			value.Task = &task
		}
		result = append(result, value)
	}
	return result, nil
}

func agentSnapshot(value service.AgentDetail) dto.AgentSnapshot {
	result := dto.AgentSnapshot{
		ID:                     value.ID.String(),
		Name:                   value.Name,
		SessionID:              value.SessionID.String(),
		DefinitionID:           value.DefinitionID.String(),
		SecurityPolicyRevision: value.SecurityPolicyRevision,
		Profile:                value.Profile,
		State:                  value.State,
		CurrentTask:            value.CurrentTaskID.String(),
		TaskIDs:                value.TaskIDs,
		Tasks:                  make([]dto.TaskSnapshot, 0, len(value.Tasks)),
		ControlCommandIDs:      value.ControlCommandIDs,
	}
	for _, task := range value.Tasks {
		result.Tasks = append(result.Tasks, taskSnapshot(task))
	}
	return result
}

func agentMessage(value service.AgentMessage) dto.AgentMessage {
	result := dto.AgentMessage{
		ID:         value.ID,
		Sequence:   value.Sequence,
		At:         value.At,
		TaskID:     value.TaskID,
		Role:       value.Role,
		AuthorKind: value.AuthorKind,
		AuthorID:   value.AuthorID,
		Content:    value.Content,
		Thinking:   value.Thinking,
		Blocks:     make([]dto.AgentMessageBlock, 0, len(value.Blocks)),
	}
	for _, block := range value.Blocks {
		result.Blocks = append(result.Blocks, dto.AgentMessageBlock{
			Kind:    block.Kind,
			Text:    block.Text,
			CallID:  block.CallID,
			Name:    block.Name,
			Input:   block.Input,
			IsError: block.IsError,
		})
	}
	return result
}

func taskSnapshot(value service.TaskInfo) dto.TaskSnapshot {
	return dto.TaskSnapshot{
		ID:             value.ID.String(),
		Status:         value.Status,
		Outcome:        value.Outcome,
		FailureCode:    value.FailureCode,
		FailureMessage: value.FailureMessage,
		CreatedAt:      value.CreatedAt,
		StartedAt:      value.StartedAt,
		EndedAt:        value.EndedAt,
	}
}
