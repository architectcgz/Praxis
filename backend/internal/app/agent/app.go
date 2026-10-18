// Package agent 提供 Agent 查询、执行控制和公开事件订阅。
package agent

import (
	"bytes"
	"context"
	"strings"

	agentcontracts "praxis/internal/app/agent/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/identity"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/request"
	businessagent "praxis/internal/service/agent"
)

// App 投影 Agent 业务结果，不持有业务状态和执行绑定。
type App struct {
	agents *businessagent.Service
	events *EventPublisher
}

// New 连接 Agent 业务和应用事件出口；事件出口允许为空。
func New(agents *businessagent.Service, events *EventPublisher) *App {
	return &App{agents: agents, events: events}
}

// GetAgentDetail 返回可展示的任务和控制状态；内部执行配置不会暴露。
func (a *App) GetAgentDetail(ctx context.Context, agentID string, limit int) (agentcontracts.AgentDetail, error) {
	limit, err := request.ListLimit(limit)
	if err != nil {
		return agentcontracts.AgentDetail{}, err
	}
	view, err := a.agents.GetAgentView(ctx, identity.AgentID(agentID), limit)
	if err != nil {
		return agentcontracts.AgentDetail{}, err
	}
	result := agentcontracts.AgentDetail{
		ID:                view.Agent.ID.String(),
		Name:              agentmodel.DisplayName(view.Agent.DefinitionID),
		SessionID:         view.Agent.SessionID.String(),
		DefinitionID:      view.Agent.DefinitionID.String(),
		Profile:           string(view.Agent.Profile),
		State:             string(view.Agent.State),
		CurrentTaskID:     view.Agent.CurrentTaskID.String(),
		TaskIDs:           make([]string, 0, len(view.Tasks)),
		Tasks:             make([]agentcontracts.TaskInfo, 0, len(view.Tasks)),
		ControlCommandIDs: make([]string, 0, len(view.Controls)),
	}
	for _, task := range view.Tasks {
		result.TaskIDs = append(result.TaskIDs, task.ID.String())
		result.Tasks = append(result.Tasks, taskInfo(task))
	}
	for _, control := range view.Controls {
		result.ControlCommandIDs = append(result.ControlCommandIDs, control.ID.String())
	}
	return result, nil
}

// ListAgentMessages 返回过滤后的公开消息；分页默认值由输入边界决定。
func (a *App) ListAgentMessages(ctx context.Context, agentID string, limit int) ([]agentcontracts.AgentMessage, error) {
	limit, err := request.ListLimit(limit)
	if err != nil {
		return nil, err
	}
	return a.listAgentMessages(ctx, agentID, limit)
}

func (a *App) listAgentMessages(ctx context.Context, agentID string, limit int) ([]agentcontracts.AgentMessage, error) {
	messages, err := a.agents.ListAgentMessages(ctx, identity.AgentID(agentID), limit)
	if err != nil {
		return nil, err
	}
	result := make([]agentcontracts.AgentMessage, 0, len(messages))
	for _, message := range messages {
		if value, visible := publicAgentMessage(message); visible {
			result = append(result, value)
		}
	}
	return result, nil
}

// PauseAgent 持久化暂停命令后通知指定任务，不在应用层重复发布终态。
func (a *App) PauseAgent(ctx context.Context, canonical request.Control) (agentcontracts.ControlResult, error) {
	result, err := a.agents.PauseAgent(ctx, controlParams(canonical))
	if err != nil {
		return agentcontracts.ControlResult{}, err
	}
	return controlResult(result), nil
}

// CancelTask 固定控制目标，延迟请求不能取消后续任务。
func (a *App) CancelTask(ctx context.Context, canonical request.Control) (agentcontracts.ControlResult, error) {
	result, err := a.agents.StopAgent(ctx, controlParams(canonical))
	if err != nil {
		return agentcontracts.ControlResult{}, err
	}
	return controlResult(result), nil
}

func controlParams(canonical request.Control) businessagent.ControlParams {
	return businessagent.ControlParams{
		CommandID:    identity.AgentControlCommandID(canonical.CommandID),
		AgentID:      identity.AgentID(canonical.AgentID),
		TargetTaskID: identity.TaskID(canonical.TargetTaskID),
	}
}

func controlResult(value businessagent.ControlResult) agentcontracts.ControlResult {
	return agentcontracts.ControlResult{
		Command: agentcontracts.ControlInfo{
			ID:           value.Command.ID.String(),
			AgentID:      value.Command.AgentID.String(),
			TargetTaskID: value.Command.TargetTaskID.String(),
			Kind:         string(value.Command.Kind),
			Status:       string(value.Command.Status),
		},
		ExistingCommand:   value.ExistingCommand,
		CancellationError: value.CancellationError,
	}
}

func taskInfo(task taskmodel.Task) agentcontracts.TaskInfo {
	return agentcontracts.TaskInfo{
		ID:             task.ID.String(),
		Status:         string(task.Status),
		Outcome:        string(task.Outcome),
		FailureCode:    string(task.FailureCode),
		FailureMessage: task.FailureMessage,
		CreatedAt:      task.CreatedAt,
		StartedAt:      task.StartedAt,
		EndedAt:        task.EndedAt,
	}
}

func publicAgentMessage(value sessionmodel.MessageData) (agentcontracts.AgentMessage, bool) {
	var content strings.Builder
	var thinking strings.Builder
	blocks := make([]agentcontracts.MessageBlock, 0, len(value.Blocks))
	for _, block := range value.Blocks {
		if block.Kind == sessionmodel.BlockText {
			content.WriteString(block.Text)
		}
		if block.Kind == sessionmodel.BlockThinking {
			thinking.WriteString(block.Text)
		}
		if block.Kind == sessionmodel.BlockText || block.Kind == sessionmodel.BlockThinking ||
			block.Kind == sessionmodel.BlockToolCall || block.Kind == sessionmodel.BlockToolResult {
			blocks = append(blocks, agentcontracts.MessageBlock{
				Kind:    string(block.Kind),
				Text:    block.Text,
				CallID:  block.CallID,
				Name:    block.Name,
				Input:   bytes.Clone(block.Input),
				IsError: block.IsError,
			})
		}
	}
	if (value.Role != sessionmodel.RoleUser && value.Role != sessionmodel.RoleAssistant && value.Role != sessionmodel.RoleTool) ||
		(strings.TrimSpace(content.String()) == "" && len(blocks) == 0 && strings.TrimSpace(thinking.String()) == "") {
		return agentcontracts.AgentMessage{}, false
	}
	return agentcontracts.AgentMessage{
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
	}, true
}
