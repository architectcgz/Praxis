package bindings

import (
	appcontext "praxis/internal/context"
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	runtimecontract "praxis/internal/runtime"
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
		ID: view.Agent.ID.String(), SessionID: view.Agent.SessionID.String(),
		DefinitionID:           view.Agent.DefinitionID.String(),
		SecurityPolicyRevision: view.Agent.SecurityPolicyRevision,
		Profile:                string(view.Agent.Profile),
		State:                  string(view.Agent.State),
		CurrentExecution:       view.Agent.CurrentExecutionID.String(),
		ExecutionIDs:           make([]string, 0, len(view.Executions)),
		Executions:             make([]dto.ExecutionSnapshot, 0, len(view.Executions)),
		WaitConditionIDs:       make([]string, 0, len(view.Waits)),
		ControlCommandIDs:      make([]string, 0, len(view.Controls)),
	}
	for _, execution := range view.Executions {
		result.ExecutionIDs = append(result.ExecutionIDs, execution.ID.String())
		result.Executions = append(result.Executions, executionSnapshot(execution))
	}
	for _, wait := range view.Waits {
		result.WaitConditionIDs = append(result.WaitConditionIDs, wait.ID.String())
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
	result := make([]dto.AgentHistoryItem, 0, len(messages)+len(view.Executions))
	for _, message := range messages {
		value, visible := publicAgentMessage(message)
		if !visible {
			continue
		}
		result = append(result, dto.AgentHistoryItem{
			Kind: "message", At: message.At, Sequence: message.Sequence, Message: &value,
		})
	}
	for _, execution := range view.Executions {
		if execution.Status != executionmodel.ExecutionSettled || execution.Outcome != executionmodel.ExecutionFailed {
			continue
		}
		at := execution.SettledAt
		if at.IsZero() {
			at = execution.CreatedAt
		}
		value := executionSnapshot(execution)
		result = append(result, dto.AgentHistoryItem{
			Kind: "execution", At: at, Execution: &value,
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

func publicAgentMessage(message runtimecontract.AgentSessionMessage) (dto.AgentMessage, bool) {
	content := message.Content
	if len(message.Blocks) > 0 {
		var text strings.Builder
		for _, block := range message.Blocks {
			if block.Kind == "text" {
				text.WriteString(block.Text)
			}
		}
		content = text.String()
	}
	blocks := publicAgentMessageBlocks(message.Blocks)
	if (message.Role != "user" && message.Role != "assistant" && message.Role != "tool") ||
		(strings.TrimSpace(content) == "" && len(blocks) == 0 && strings.TrimSpace(message.Thinking) == "") {
		return dto.AgentMessage{}, false
	}
	return dto.AgentMessage{
		Sequence: message.Sequence, At: message.At, ExecutionID: message.ExecutionID.String(),
		Role: message.Role, Content: content, Thinking: message.Thinking, Blocks: blocks,
	}, true
}

func publicAgentMessageBlocks(blocks []appcontext.ContextBlock) []dto.AgentMessageBlock {
	if len(blocks) == 0 {
		return nil
	}
	result := make([]dto.AgentMessageBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Kind != appcontext.ContextBlockText && block.Kind != appcontext.ContextBlockThinking && block.Kind != appcontext.ContextBlockToolCall &&
			block.Kind != appcontext.ContextBlockToolResult {
			continue
		}
		result = append(result, dto.AgentMessageBlock{
			Kind: string(block.Kind), Text: block.Text, CallID: block.CallID, Name: block.Name,
			Input: append([]byte(nil), block.Input...), IsError: block.IsError,
		})
	}
	return result
}

func executionSnapshot(execution executionmodel.AgentExecution) dto.ExecutionSnapshot {
	return dto.ExecutionSnapshot{
		ID: execution.ID.String(), Reason: string(execution.Reason), Status: string(execution.Status),
		Outcome: string(execution.Outcome), FailureCode: string(execution.FailureCode),
		CreatedAt: execution.CreatedAt, StartedAt: execution.StartedAt, SettledAt: execution.SettledAt,
	}
}
