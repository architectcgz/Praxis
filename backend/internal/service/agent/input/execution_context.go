package input

import (
	"context"
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"praxis/internal/identity"
	agentmodel "praxis/internal/service/agent"
	sessionmodel "praxis/internal/service/session"

	queued "praxis/internal/core/task"
)

// selectContext 在准入事务内冻结历史边界、排除项和共享条目版本，并检查必要原文容量。
// 容量检查是准入的业务前置条件：连 Protected 原文都放不进窗口的 Execution 不允许进入执行。
func (s *Service) selectContext(ctx context.Context, agent agentmodel.Agent, config agentmodel.ExecutionConfig, inputMessageID string) (agentmodel.ContextSelection, error) {
	if err := s.reconcileToolResults(ctx, agent.SessionID, agent.ID, false); err != nil {
		return agentmodel.ContextSelection{}, err
	}
	var selection agentmodel.ContextSelection
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
		if message.Sequence > stream.SequenceBoundary || message.Role != messagemodel.RoleUser || message.SourceTaskID == "" || message.ID == inputMessageID {
			continue
		}
		task, err := s.queue.Get(ctx, identity.TaskID(message.SourceTaskID))
		if err != nil {
			return agentmodel.ContextSelection{}, err
		}
		if task.Status == queued.Pending {
			selection.ExcludedMessageIDs = append(selection.ExcludedMessageIDs, message.ID)
		}
	}
	for _, entry := range shared {
		selection.SessionContextRefs = append(selection.SessionContextRefs, agentmodel.ContextReference{ID: entry.ID.String(), Digest: entry.ContentDigest})
	}
	messages, _ := selectExecutionMessages(stream.Messages, selection, inputMessageID, "")
	content, err := contextContent(config.SystemPrompt, inputMessageID, shared, messages, nil)
	if err != nil {
		return agentmodel.ContextSelection{}, err
	}
	// 结果只用于容量前置条件；上下文结构由消息写入与恢复边界保证，不在此复查。
	if _, err := (appcontext.ContextBuilder{}).Build(content, appcontext.Limits{ContextWindow: config.Model.ContextWindow, MaxOutputTokens: config.Model.MaxOutputTokens}); err != nil {
		return agentmodel.ContextSelection{}, err
	}
	return selection, nil
}

// selectExecutionMessages 选择边界内历史、当前输入和本 Execution 的续接消息。
// 边界之后补写的历史回执只按 (ExecutionID, CallID) 关联已选调用。
func selectExecutionMessages(messages []messagemodel.Message, selection agentmodel.ContextSelection, inputMessageID string, executionID string) (
	[]messagemodel.Message, []messagemodel.Message,
) {
	excluded := make(map[string]bool, len(selection.ExcludedMessageIDs))
	for _, id := range selection.ExcludedMessageIDs {
		excluded[id] = true
	}
	selectedHistory := func(message messagemodel.Message) bool {
		return message.Sequence <= selection.MessageSequenceBoundary && !excluded[message.ID]
	}
	type callKey struct{ executionID, callID string }
	selectedToolCalls := make(map[callKey]bool)
	for _, message := range messages {
		if !selectedHistory(message) || message.Role != messagemodel.RoleAssistant {
			continue
		}
		for _, block := range message.Blocks {
			if block.Kind == messagemodel.BlockToolCall {
				selectedToolCalls[callKey{message.ExecutionID, block.CallID}] = true
			}
		}
	}
	selectedMessages := make([]messagemodel.Message, 0)
	continuation := make([]messagemodel.Message, 0)
	for _, message := range messages {
		if excluded[message.ID] {
			continue
		}
		if executionID != "" && message.ExecutionID == executionID && message.ID != inputMessageID {
			if message.Role == messagemodel.RoleAssistant || message.Role == messagemodel.RoleTool {
				continuation = append(continuation, message)
			}
			continue
		}
		include := selectedHistory(message) || message.ID == inputMessageID
		if message.Role == messagemodel.RoleTool {
			// 只纳入已选调用对应的回执块，避免一个消息内混入未选调用。
			blocks := make([]messagemodel.Block, 0, len(message.Blocks))
			for _, block := range message.Blocks {
				if block.Kind == messagemodel.BlockToolResult && selectedToolCalls[callKey{message.ExecutionID, block.CallID}] {
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

func (s *Service) loadContextEntries(ctx context.Context, sessionID identity.SessionID) ([]sessionmodel.SessionContextEntry, error) {
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

// LoadContext 读取冻结历史与本 Execution 已提交的续接消息，只输出通用模型内容。
// 业务恢复先协调已停止的工具调用；消息 Repository 读取保持只读。
func (s *Service) LoadContext(ctx context.Context, task agentmodel.Execution) (appcontext.Content, []messagemodel.Message, error) {
	if ctx == nil {
		return appcontext.Content{}, nil, errors.New("task context is required")
	}
	var content appcontext.Content
	var continuation []messagemodel.Message
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		if err := s.reconcileToolResults(txCtx, task.SessionID, task.AgentID, false); err != nil {
			return err
		}
		stream, err := s.messages.LoadMessages(txCtx, task.SessionID, task.AgentID, 0)
		if err != nil {
			return err
		}
		messages, next := selectExecutionMessages(stream.Messages, task.ContextSelection, task.InputMessageID, task.ID.String())
		shared := make([]sessionmodel.SessionContextEntry, 0, len(task.ContextSelection.SessionContextRefs))
		for _, ref := range task.ContextSelection.SessionContextRefs {
			entry, ok, err := s.contexts.GetByID(txCtx, identity.ContextEntryID(ref.ID))
			if err != nil {
				return err
			}
			if !ok || entry.SessionID != task.SessionID || entry.ContentDigest != ref.Digest {
				return fmt.Errorf("frozen context entry %s: %w", ref.ID, fault.ErrRevisionConflict)
			}
			shared = append(shared, entry)
		}
		content, err = contextContent(task.Config.SystemPrompt, task.InputMessageID, shared, messages, next)
		continuation = next
		return err
	})
	if err != nil {
		return appcontext.Content{}, nil, err
	}
	return content, continuation, nil
}
