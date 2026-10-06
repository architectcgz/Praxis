package bindings

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"sort"
	"strings"
)

type AgentBindings struct {
	runtime Runtime
}

func (b *AgentBindings) GetAgent(agentID string) (dto.AgentSnapshot, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return dto.AgentSnapshot{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.AgentSnapshot{}, err
	}
	view, err := service.Agents.GetAgentView(ctx, contracts.AgentID(agentID), 100)
	if err != nil {
		return dto.AgentSnapshot{}, publicError(b.runtime, "AgentBindings.GetAgent", err)
	}
	result := dto.AgentSnapshot{
		ID: view.Agent.ID.String(), Name: agentmodel.DisplayName(view.Agent.DefinitionID), SessionID: view.Agent.SessionID.String(),
		DefinitionID:           view.Agent.DefinitionID.String(),
		SecurityPolicyRevision: view.Agent.SecurityPolicyRevision,
		Profile:                string(view.Agent.Profile),
		State:                  string(view.Agent.State),
		CurrentTask:            view.Agent.CurrentTaskID.String(),
		TaskIDs:                make([]string, 0, len(view.Tasks)),
		Tasks:                  make([]dto.TaskSnapshot, 0, len(view.Tasks)),
		ControlCommandIDs:      make([]string, 0, len(view.Controls)),
	}
	for _, task := range view.Tasks {
		result.TaskIDs = append(result.TaskIDs, task.ID.String())
		result.Tasks = append(result.Tasks, taskSnapshot(task))
	}
	for _, control := range view.Controls {
		result.ControlCommandIDs = append(result.ControlCommandIDs, control.ID.String())
	}
	return result, nil
}

func (b *AgentBindings) ListAgentMessages(agentID string) ([]dto.AgentMessage, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	messages, err := service.Agents.ListAgentMessages(ctx, contracts.AgentID(agentID), 200)
	if err != nil {
		return nil, publicError(b.runtime, "AgentBindings.ListAgentMessages", err)
	}
	result := make([]dto.AgentMessage, 0, len(messages))
	for _, message := range messages {
		if visible, ok := publicAgentMessage(message); ok {
			result = append(result, visible)
		}
	}
	return result, nil
}

func (b *AgentBindings) ListAgentHistory(agentID string) ([]dto.AgentHistoryItem, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	messages, err := service.Agents.ListAgentMessages(ctx, contracts.AgentID(agentID), 200)
	if err != nil {
		return nil, publicError(b.runtime, "AgentBindings.ListAgentHistory.messages", err)
	}
	view, err := service.Agents.GetAgentView(ctx, contracts.AgentID(agentID), 200)
	if err != nil {
		return nil, publicError(b.runtime, "AgentBindings.ListAgentHistory.view", err)
	}
	result := make([]dto.AgentHistoryItem, 0, len(messages)+len(view.Tasks))
	for _, message := range messages {
		value, visible := publicAgentMessage(message)
		if !visible {
			continue
		}
		result = append(result, dto.AgentHistoryItem{
			Kind: "message", At: message.CreatedAt, Sequence: message.Sequence, Message: &value,
		})
	}
	for _, task := range view.Tasks {
		if task.Status != taskmodel.TaskEnded {
			continue
		}
		kind := "task"
		if task.FailureCode == contracts.TaskFailureRequestCanceled {
			kind = "request_canceled"
		} else if task.Outcome != taskmodel.TaskFailed {
			continue
		}
		at := task.EndedAt
		if at.IsZero() {
			at = task.CreatedAt
		}
		value := taskSnapshot(task)
		result = append(result, dto.AgentHistoryItem{
			Kind: kind, At: at, Task: &value,
		})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if result[i].At.Equal(result[j].At) {
			return result[i].Sequence < result[j].Sequence
		}
		return result[i].At.Before(result[j].At)
	})
	return result, nil
}

func publicAgentMessage(value sessionmodel.MessageData) (dto.AgentMessage, bool) {
	var content strings.Builder
	var thinking strings.Builder
	blocks := make([]dto.AgentMessageBlock, 0, len(value.Blocks))
	for _, block := range value.Blocks {
		if block.Kind == sessionmodel.BlockText {
			content.WriteString(block.Text)
		}
		if block.Kind == sessionmodel.BlockThinking {
			thinking.WriteString(block.Text)
		}
		if block.Kind == sessionmodel.BlockText || block.Kind == sessionmodel.BlockThinking ||
			block.Kind == sessionmodel.BlockToolCall || block.Kind == sessionmodel.BlockToolResult {
			blocks = append(blocks, dto.AgentMessageBlock{
				Kind: string(block.Kind), Text: block.Text, CallID: block.CallID, Name: block.Name,
				Input: append([]byte(nil), block.Input...), IsError: block.IsError,
			})
		}
	}
	if (value.Role != sessionmodel.RoleUser && value.Role != sessionmodel.RoleAssistant && value.Role != sessionmodel.RoleTool) ||
		(strings.TrimSpace(content.String()) == "" && len(blocks) == 0 && strings.TrimSpace(thinking.String()) == "") {
		return dto.AgentMessage{}, false
	}
	return dto.AgentMessage{
		ID:       value.ID,
		Sequence: value.Sequence, At: value.CreatedAt, TaskID: value.TaskID,
		Role: string(value.Role), AuthorKind: string(value.AuthorKind), AuthorID: value.AuthorID,
		Content: content.String(), Thinking: thinking.String(), Blocks: blocks,
	}, true
}

func taskSnapshot(task taskmodel.Task) dto.TaskSnapshot {
	return dto.TaskSnapshot{
		ID: task.ID.String(), Status: string(task.Status),
		Outcome: string(task.Outcome), FailureCode: string(task.FailureCode),
		FailureMessage: task.FailureMessage,
		CreatedAt:      task.CreatedAt, StartedAt: task.StartedAt, EndedAt: task.EndedAt,
	}
}
