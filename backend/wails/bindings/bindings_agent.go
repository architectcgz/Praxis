package bindings

import (
	"bytes"
	"strings"

	"praxis/internal/agent"
	"praxis/internal/identity"
	"praxis/internal/message"
	"praxis/internal/task"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

type AgentBindings struct{ runtime Runtime }

func (b *AgentBindings) GetAgent(agentID string) (dto.AgentSnapshot, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return dto.AgentSnapshot{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.AgentSnapshot{}, err
	}
	view, err := services.Agents.GetAgentView(ctx, identity.AgentID(agentID), 100)
	if err != nil {
		return dto.AgentSnapshot{}, publicError(b.runtime, "AgentBindings.GetAgent", err)
	}
	result := agentSnapshot(view.Agent)
	result.TaskIDs = make([]string, 0, len(view.Tasks))
	result.ControlCommandIDs = make([]string, 0, len(view.Controls))
	for _, value := range view.Tasks {
		result.TaskIDs = append(result.TaskIDs, value.ID.String())
		result.Tasks = append(result.Tasks, taskSnapshot(value))
	}
	for _, value := range view.Controls {
		result.ControlCommandIDs = append(result.ControlCommandIDs, value.ID.String())
	}
	return result, nil
}

func (b *AgentBindings) ListAgentMessages(agentID string) ([]dto.AgentMessage, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	messages, err := services.Agents.ListAgentMessages(ctx, identity.AgentID(agentID), 200)
	if err != nil {
		return nil, publicError(b.runtime, "AgentBindings.ListAgentMessages", err)
	}
	return publicAgentMessages(messages), nil
}

func (b *AgentBindings) ListAgentHistory(agentID, before string) (dto.AgentHistoryPage, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return dto.AgentHistoryPage{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.AgentHistoryPage{}, err
	}
	page, err := listAgentHistory(ctx, services.Agents, identity.AgentID(agentID), before, 200)
	if err != nil {
		return dto.AgentHistoryPage{}, publicError(b.runtime, "AgentBindings.ListAgentHistory", err)
	}
	return page, nil
}

func agentSnapshot(value agent.Agent) dto.AgentSnapshot {
	return dto.AgentSnapshot{
		ID:           value.ID.String(),
		Name:         agent.DisplayName(value.DefinitionID),
		SessionID:    value.SessionID.String(),
		DefinitionID: value.DefinitionID.String(),
		Profile:      string(value.Profile),
		State:        string(value.State),
		CurrentTask:  value.CurrentTaskID.String(),
		Tasks:        []dto.TaskSnapshot{},
	}
}

// publicAgentMessages 只投影可展示内容，opaque 推理状态不进入前端协议。
func publicAgentMessages(values []message.Message) []dto.AgentMessage {
	result := make([]dto.AgentMessage, 0, len(values))
	for _, value := range values {
		var content, thinking strings.Builder
		blocks := make([]dto.AgentMessageBlock, 0, len(value.Blocks))
		for _, block := range value.Blocks {
			if block.Kind == message.BlockText {
				content.WriteString(block.Text)
			}
			if block.Kind == message.BlockThinking {
				thinking.WriteString(block.Text)
			}
			if block.Kind == message.BlockText || block.Kind == message.BlockThinking || block.Kind == message.BlockToolCall || block.Kind == message.BlockToolResult {
				blocks = append(blocks, dto.AgentMessageBlock{
					Kind:    string(block.Kind),
					Text:    block.Text,
					CallID:  block.CallID,
					Name:    block.Name,
					Input:   bytes.Clone(block.Input),
					IsError: block.IsError,
				})
			}
		}
		if (value.Role != message.RoleUser && value.Role != message.RoleAssistant && value.Role != message.RoleTool) ||
			(strings.TrimSpace(content.String()) == "" && len(blocks) == 0 && strings.TrimSpace(thinking.String()) == "") {
			continue
		}
		result = append(result, dto.AgentMessage{
			ID:         value.ID,
			Sequence:   value.Sequence,
			At:         value.CreatedAt,
			TaskID:     value.TaskID,
			Role:       string(value.Role),
			AuthorKind: string(value.AuthorKind),
			AuthorID:   value.AuthorID,
			Content:    content.String(),
			Thinking:   thinking.String(),
			Blocks:     blocks,
		})
	}
	return result
}

func taskSnapshot(value task.Task) dto.TaskSnapshot {
	return dto.TaskSnapshot{
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
