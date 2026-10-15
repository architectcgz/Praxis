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

// ContextBuilderConfig 提供 Agent 所属消息流和共享条目的读取能力，以及恢复时的事务边界。
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

// BuildInitialContext 在调用方准入事务内确定历史边界、排除项和共享条目版本。
// 构建成功后返回完整执行输入；失败不修改传入的 Input。
func (b *TaskContextBuilder) BuildInitialContext(
	ctx context.Context,
	agent agentmodel.Agent,
	input taskmodel.Input,
	currentInput string,
) (taskmodel.Input, error) {
	if ctx == nil {
		return taskmodel.Input{}, errors.New("task context build context is required")
	}
	if err := ctx.Err(); err != nil {
		return taskmodel.Input{}, err
	}
	if _, err := b.sessions.Get(ctx, agent.SessionID); err != nil {
		return taskmodel.Input{}, err
	}
	var entries []contextmodel.Entry
	var err error
	if agent.CanReadSessionContext() {
		entries, err = b.loadContextEntries(ctx, agent.SessionID)
		if err != nil {
			return taskmodel.Input{}, err
		}
	}
	stream, err := b.messages.LoadMessages(ctx, agent.SessionID, agent.ID, 0)
	if err != nil {
		return taskmodel.Input{}, err
	}
	// 排除结果在启动时确定，恢复时不再查询其他 Task 的状态。
	excludedMessageIDs := make([]string, 0)
	for _, message := range stream.Messages {
		if message.Sequence > stream.SequenceBoundary || message.Role != sessionmodel.RoleUser ||
			message.TaskID == "" || message.ID == input.CurrentInputMessageID {
			continue
		}
		task, err := b.tasks.Get(ctx, contracts.TaskID(message.TaskID))
		if err != nil {
			return taskmodel.Input{}, err
		}
		if task.Status == taskmodel.TaskPending {
			excludedMessageIDs = append(excludedMessageIDs, message.ID)
		}
	}
	input.MessageSequenceBoundary = stream.SequenceBoundary
	input.ExcludedMessageIDs = excludedMessageIDs
	selectedMessages, _ := selectTaskMessages(stream.Messages, input, "")
	if _, err = b.contextBuilder.Build(contextmodel.BuildInput{
		ContextWindow:         input.Model.ContextWindow,
		MaxOutputTokens:       input.Model.MaxOutputTokens,
		SystemPrompt:          input.SystemPrompt,
		Entries:               entries,
		Messages:              selectedMessages,
		CurrentInput:          currentInput,
		CurrentInputMessageID: input.CurrentInputMessageID,
	}); err != nil {
		return taskmodel.Input{}, err
	}
	input.SessionContextRefs = make([]taskmodel.ContextReference, 0, len(entries))
	for _, entry := range entries {
		input.SessionContextRefs = append(input.SessionContextRefs, taskmodel.ContextReference{
			ID:     entry.ID,
			Digest: entry.ContentDigest,
		})
	}
	return input, nil
}

// selectTaskMessages 选择边界内历史、当前输入和本 Task 的续接消息。
// 边界之后补写的历史回执只按 (TaskID, CallID) 关联已选调用。
func selectTaskMessages(messages []sessionmodel.MessageData, input taskmodel.Input, taskID string) (
	[]sessionmodel.MessageData, []sessionmodel.MessageData,
) {
	excluded := make(map[string]bool, len(input.ExcludedMessageIDs))
	for _, id := range input.ExcludedMessageIDs {
		excluded[id] = true
	}
	selectedHistory := func(message sessionmodel.MessageData) bool {
		return message.Sequence <= input.MessageSequenceBoundary && !excluded[message.ID]
	}
	type callKey struct{ taskID, callID string }
	selectedToolCalls := make(map[callKey]bool)
	for _, message := range messages {
		if !selectedHistory(message) || message.Role != sessionmodel.RoleAssistant {
			continue
		}
		for _, block := range message.Blocks {
			if block.Kind == sessionmodel.BlockToolCall {
				selectedToolCalls[callKey{message.TaskID, block.CallID}] = true
			}
		}
	}
	selectedMessages := make([]sessionmodel.MessageData, 0)
	continuation := make([]sessionmodel.MessageData, 0)
	for _, message := range messages {
		if taskID != "" && message.TaskID == taskID && message.ID != input.CurrentInputMessageID {
			if message.Role == sessionmodel.RoleAssistant || message.Role == sessionmodel.RoleTool {
				continuation = append(continuation, message)
			}
			continue
		}
		include := selectedHistory(message) || message.ID == input.CurrentInputMessageID
		if !include && message.Role == sessionmodel.RoleTool {
			// 只纳入已选调用对应的回执块，避免一个消息内混入未选调用。
			blocks := make([]sessionmodel.Block, 0, len(message.Blocks))
			for _, block := range message.Blocks {
				if block.Kind == sessionmodel.BlockToolResult && selectedToolCalls[callKey{message.TaskID, block.CallID}] {
					blocks = append(blocks, block)
				}
			}
			message.Blocks = blocks
			include = len(blocks) > 0
		}
		if include {
			selectedMessages = append(selectedMessages, message)
		}
	}
	return selectedMessages, continuation
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

// RebuildTaskContext 在同一事务内重建启动时选定的历史与本 Task 的已提交续接消息。
// 冻结条目缺失或 digest 不符时返回 ErrRevisionConflict；缺失工具回执由消息读取 Port 修复。
func (b *TaskContextBuilder) RebuildTaskContext(ctx context.Context, task taskmodel.Task) (contextmodel.BuildResult, error) {
	var result contextmodel.BuildResult
	err := b.tx.InTx(ctx, func(txCtx context.Context) error {
		stream, err := b.messages.LoadMessages(txCtx, task.SessionID, task.AgentID, 0)
		if err != nil {
			return err
		}
		selectedMessages, continuation := selectTaskMessages(stream.Messages, task.Input, task.ID.String())
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
			ContextWindow:         task.Input.Model.ContextWindow,
			MaxOutputTokens:       task.Input.Model.MaxOutputTokens,
			SystemPrompt:          task.Input.SystemPrompt,
			Entries:               entries,
			Messages:              selectedMessages,
			CurrentInputMessageID: task.Input.CurrentInputMessageID,
			ContinuationMessages:  continuation,
		})
		result.TaskMessages = continuation
		return err
	})
	return result, err
}
