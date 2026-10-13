package task

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	contextmodel "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/repository"

	"context"
	"errors"
	"fmt"
)

// ContextBuilderConfig 提供 Task 可见消息和共享条目的读取能力，以及恢复时的事务边界。
type ContextBuilderConfig struct {
	Transactions repository.TxRunner
	Sessions     repository.SessionRepository
	Tasks        repository.TaskRepository
	Contexts     repository.SessionContextRepository
	Messages     repository.MessageLoader
}

// TaskContextBuilder 加载 Task 初始与恢复上下文；模型投影的排序和配对由 core/context 完成。
type TaskContextBuilder struct {
	tx             repository.TxRunner
	sessions       repository.SessionRepository
	tasks          repository.TaskRepository
	contexts       repository.SessionContextRepository
	messages       repository.MessageLoader
	contextBuilder contextmodel.ContextBuilder
}

// NewTaskContextBuilder 创建可共用于 Task 准入和 Loop 恢复的构建器；缺少依赖时返回错误。
func NewTaskContextBuilder(config ContextBuilderConfig) (*TaskContextBuilder, error) {
	for name, value := range map[string]any{
		"transactions": config.Transactions,
		"sessions":     config.Sessions,
		"tasks":        config.Tasks,
		"contexts":     config.Contexts,
		"messages":     config.Messages,
	} {
		if value == nil {
			return nil, fmt.Errorf("task context builder %s is required", name)
		}
	}
	return &TaskContextBuilder{
		tx:             config.Transactions,
		sessions:       config.Sessions,
		tasks:          config.Tasks,
		contexts:       config.Contexts,
		messages:       config.Messages,
		contextBuilder: contextmodel.NewContextBuilder(),
	}, nil
}

// BuildInitialContext 在调用方 Task 准入事务内选择可见历史并冻结共享条目引用。
// 后续排队输入不进入模型上下文；消息读取或纯转换失败时返回错误，不修改 Task。
func (b *TaskContextBuilder) BuildInitialContext(
	ctx context.Context,
	agent agentmodel.Agent,
	systemPrompt string,
	currentInput string,
	currentInputMessageID string,
	contextWindow int,
	maxOutputTokens int,
) (contextmodel.BuildResult, error) {
	if ctx == nil {
		return contextmodel.BuildResult{}, errors.New("task context build context is required")
	}
	if err := ctx.Err(); err != nil {
		return contextmodel.BuildResult{}, err
	}
	if _, err := b.sessions.Get(ctx, agent.SessionID); err != nil {
		return contextmodel.BuildResult{}, err
	}
	var err error
	var entries []contextmodel.Entry
	if agent.CanReadSessionContext() {
		entries, err = b.loadContextEntries(ctx, agent.SessionID)
		if err != nil {
			return contextmodel.BuildResult{}, err
		}
	}
	stream, err := b.messages.LoadMessages(ctx, agent.SessionID, agent.ID, 0)
	if err != nil {
		return contextmodel.BuildResult{}, err
	}
	// 预约输入可以显示在会话中，但只有当前待执行输入能进入模型上下文，不能提前执行后续指令。
	visible := stream.Messages[:0]
	for _, message := range stream.Messages {
		if message.Role == sessionmodel.RoleUser && message.TaskID != "" && message.ID != currentInputMessageID {
			task, err := b.tasks.Get(ctx, contracts.TaskID(message.TaskID))
			if err != nil {
				return contextmodel.BuildResult{}, err
			}
			if task.Status == taskmodel.TaskPending {
				continue
			}
		}
		visible = append(visible, message)
	}
	return b.contextBuilder.Build(contextmodel.BuildInput{
		ContextWindow:           contextWindow,
		MaxOutputTokens:         maxOutputTokens,
		SystemPrompt:            systemPrompt,
		Entries:                 entries,
		MessageSequenceBoundary: stream.SequenceBoundary,
		Messages:                visible,
		CurrentInput:            currentInput,
		CurrentInputMessageID:   currentInputMessageID,
	})
}

func (b *TaskContextBuilder) loadContextEntries(
	ctx context.Context,
	sessionID contracts.SessionID,
) ([]contextmodel.Entry, error) {
	entries := make([]contextmodel.Entry, 0)
	for afterID := ""; ; {
		page, err := b.contexts.List(ctx, sessionID, afterID, 512)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return entries, nil
		}
		for _, entry := range page {
			entries = append(entries, contextmodel.Entry{
				ID:            entry.ID.String(),
				SessionID:     entry.SessionID.String(),
				Kind:          contextmodel.EntryKind(entry.Kind),
				SourceTaskID:  entry.SourceTaskID.String(),
				Content:       entry.Content,
				ContentDigest: entry.ContentDigest,
				CreatedAt:     entry.CreatedAt,
			})
		}
		afterID = page[len(page)-1].ID.String()
	}
}

// RebuildTaskContext 在同一事务内重建冻结历史与本 Task 的已提交续接消息。
// 冻结条目缺失或 digest 不符时返回 ErrRevisionConflict；缺失工具回执由消息读取 Port 修复。
func (b *TaskContextBuilder) RebuildTaskContext(ctx context.Context, task taskmodel.Task) (contextmodel.BuildResult, error) {
	var result contextmodel.BuildResult
	err := b.tx.InTx(ctx, func(txCtx context.Context) error {
		stream, err := b.messages.LoadMessages(txCtx, task.SessionID, task.AgentID, 0)
		if err != nil {
			return err
		}
		visible := make(map[string]bool, len(task.Input.VisibleMessageIDs))
		for _, id := range task.Input.VisibleMessageIDs {
			visible[id] = true
		}
		type callKey struct{ task, call string }
		visibleCalls := make(map[callKey]bool)
		for _, message := range stream.Messages {
			if !visible[message.ID] {
				continue
			}
			for _, block := range message.Blocks {
				if block.Kind == sessionmodel.BlockToolCall {
					visibleCalls[callKey{message.TaskID, block.CallID}] = true
				}
			}
		}
		history := make([]sessionmodel.MessageData, 0)
		continuation := make([]sessionmodel.MessageData, 0)
		for _, message := range stream.Messages {
			if message.TaskID == task.ID.String() && message.ID != task.Input.CurrentInputMessageID {
				continuation = append(continuation, message)
				continue
			}
			include := visible[message.ID] || message.ID == task.Input.CurrentInputMessageID
			if message.Role == sessionmodel.RoleTool {
				for _, block := range message.Blocks {
					include = include || visibleCalls[callKey{message.TaskID, block.CallID}]
				}
			}
			if include {
				history = append(history, message)
			}
		}
		allEntries, err := b.loadContextEntries(txCtx, task.SessionID)
		if err != nil {
			return err
		}
		byID := make(map[string]contextmodel.Entry, len(allEntries))
		for _, entry := range allEntries {
			byID[entry.ID] = entry
		}
		entries := make([]contextmodel.Entry, 0, len(task.Input.SessionContextRefs))
		for _, ref := range task.Input.SessionContextRefs {
			entry, ok := byID[ref.ID]
			if !ok || entry.ContentDigest != ref.Digest {
				return fmt.Errorf("frozen context entry %s: %w", ref.ID, contracts.ErrRevisionConflict)
			}
			entries = append(entries, entry)
		}
		result, err = b.contextBuilder.Build(contextmodel.BuildInput{
			ContextWindow:           task.Input.Model.ContextWindow,
			MaxOutputTokens:         task.Input.Model.MaxOutputTokens,
			SystemPrompt:            task.Input.SystemPrompt,
			Entries:                 entries,
			Messages:                history,
			MessageSequenceBoundary: task.Input.MessageSequenceBoundary,
			CurrentInputMessageID:   task.Input.CurrentInputMessageID,
			ContinuationMessages:    continuation,
		})
		result.TaskMessages = continuation
		return err
	})
	return result, err
}
