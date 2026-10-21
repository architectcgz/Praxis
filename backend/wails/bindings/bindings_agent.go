package bindings

import (
	"bytes"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/message"
	"praxis/internal/identity"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
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

func (b *AgentBindings) ListAgentMessages(agentID string) ([]dto.AgentMessage, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	messages, err := services.Agents.ListAgentMessages(ctx, identity.AgentID(agentID), 200)
	if err != nil {
		return nil, publicError(b.host, "AgentBindings.ListAgentMessages", err)
	}
	return publicAgentMessages(messages), nil
}

func (b *AgentBindings) ListAgentHistory(agentID, before string) (dto.AgentHistoryPage, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return dto.AgentHistoryPage{}, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.AgentHistoryPage{}, err
	}
	page, err := listAgentHistory(ctx, services.Agents, identity.AgentID(agentID), before, 200)
	if err != nil {
		return dto.AgentHistoryPage{}, publicError(b.host, "AgentBindings.ListAgentHistory", err)
	}
	return page, nil
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
			SourceTaskID: value.SourceTaskID,
			ID:           value.ID,
			Sequence:     value.Sequence,
			At:           value.CreatedAt,
			ExecutionID:  value.ExecutionID,
			Role:         string(value.Role),
			AuthorKind:   string(value.AuthorKind),
			AuthorID:     value.AuthorID,
			Content:      content.String(),
			Thinking:     thinking.String(),
			Blocks:       blocks,
		})
	}
	return result
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
