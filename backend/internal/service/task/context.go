package task

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	contextmodel "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
)

// selectContext 在准入事务内冻结历史边界、排除项和共享条目版本，并检查必要原文容量。
// 容量检查是准入的业务前置条件：连 Protected 原文都放不进窗口的 Task 不允许进入执行。
func (s *Service) selectContext(ctx context.Context, agent agentmodel.Agent, config taskmodel.Config, inputMessageID string) (taskmodel.ContextSelection, error) {
	if err := s.reconcileToolResults(ctx, agent.SessionID, agent.ID, false); err != nil {
		return taskmodel.ContextSelection{}, err
	}
	var selection taskmodel.ContextSelection
	var shared []sessionmodel.SessionContextEntry
	var err error
	if agent.CanReadSessionContext() {
		shared, err = s.loadContextEntries(ctx, agent.SessionID)
		if err != nil {
			return selection, err
		}
	}
	stream, err := s.messages.LoadMessages(ctx, agent.SessionID, agent.ID, 0)
	if err != nil {
		return selection, err
	}
	selection.MessageSequenceBoundary = stream.SequenceBoundary
	for _, message := range stream.Messages {
		if message.Sequence > stream.SequenceBoundary || message.Role != sessionmodel.RoleUser || message.TaskID == "" || message.ID == inputMessageID {
			continue
		}
		task, err := s.tasks.Get(ctx, contracts.TaskID(message.TaskID))
		if err != nil {
			return taskmodel.ContextSelection{}, err
		}
		if task.Status == taskmodel.TaskPending {
			selection.ExcludedMessageIDs = append(selection.ExcludedMessageIDs, message.ID)
		}
	}
	for _, entry := range shared {
		selection.SessionContextRefs = append(selection.SessionContextRefs, taskmodel.ContextReference{ID: entry.ID.String(), Digest: entry.ContentDigest})
	}
	messages, _ := selectTaskMessages(stream.Messages, selection, inputMessageID, "")
	content, err := contextContent(config.SystemPrompt, inputMessageID, shared, messages, nil)
	if err != nil {
		return taskmodel.ContextSelection{}, err
	}
	// 结果只用于容量前置条件；上下文结构由消息写入与恢复边界保证，不在此复查。
	if _, err := (contextmodel.ContextBuilder{}).Build(content, contextmodel.Limits{ContextWindow: config.Model.ContextWindow, MaxOutputTokens: config.Model.MaxOutputTokens}); err != nil {
		return taskmodel.ContextSelection{}, err
	}
	return selection, nil
}

// selectTaskMessages 选择边界内历史、当前输入和本 Task 的续接消息。
// 边界之后补写的历史回执只按 (TaskID, CallID) 关联已选调用。
func selectTaskMessages(messages []sessionmodel.MessageData, selection taskmodel.ContextSelection, inputMessageID string, taskID string) (
	[]sessionmodel.MessageData, []sessionmodel.MessageData,
) {
	excluded := make(map[string]bool, len(selection.ExcludedMessageIDs))
	for _, id := range selection.ExcludedMessageIDs {
		excluded[id] = true
	}
	selectedHistory := func(message sessionmodel.MessageData) bool {
		return message.Sequence <= selection.MessageSequenceBoundary && !excluded[message.ID]
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
		if excluded[message.ID] {
			continue
		}
		if taskID != "" && message.TaskID == taskID && message.ID != inputMessageID {
			if message.Role == sessionmodel.RoleAssistant || message.Role == sessionmodel.RoleTool {
				continuation = append(continuation, message)
			}
			continue
		}
		include := selectedHistory(message) || message.ID == inputMessageID
		if message.Role == sessionmodel.RoleTool {
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

func (s *Service) loadContextEntries(ctx context.Context, sessionID contracts.SessionID) ([]sessionmodel.SessionContextEntry, error) {
	var entries []sessionmodel.SessionContextEntry
	for afterID := ""; ; {
		page, err := s.contexts.List(ctx, sessionID, afterID, 512)
		if err != nil {
			return nil, err
		}
		if len(page) == 0 {
			return entries, nil
		}
		entries = append(entries, page...)
		afterID = page[len(page)-1].ID.String()
	}
}

// LoadContext 读取冻结历史与本 Task 已提交的续接消息，只输出通用模型内容。
// 业务恢复先协调已停止的工具调用；消息 Repository 读取保持只读。
func (s *Service) LoadContext(ctx context.Context, task taskmodel.Task) (contextmodel.Content, []sessionmodel.MessageData, error) {
	if ctx == nil {
		return contextmodel.Content{}, nil, errors.New("task context is required")
	}
	var content contextmodel.Content
	var continuation []sessionmodel.MessageData
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		if err := s.reconcileToolResults(txCtx, task.SessionID, task.AgentID, false); err != nil {
			return err
		}
		stream, err := s.messages.LoadMessages(txCtx, task.SessionID, task.AgentID, 0)
		if err != nil {
			return err
		}
		messages, next := selectTaskMessages(stream.Messages, task.ContextSelection, task.InputMessageID, task.ID.String())
		shared := make([]sessionmodel.SessionContextEntry, 0, len(task.ContextSelection.SessionContextRefs))
		for _, ref := range task.ContextSelection.SessionContextRefs {
			entry, ok, err := s.contexts.GetByID(txCtx, contracts.ContextEntryID(ref.ID))
			if err != nil {
				return err
			}
			if !ok || entry.SessionID != task.SessionID || entry.ContentDigest != ref.Digest {
				return fmt.Errorf("frozen context entry %s: %w", ref.ID, contracts.ErrRevisionConflict)
			}
			shared = append(shared, entry)
		}
		content, err = contextContent(task.Config.SystemPrompt, task.InputMessageID, shared, messages, next)
		continuation = next
		return err
	})
	if err != nil {
		return contextmodel.Content{}, nil, err
	}
	return content, continuation, nil
}
