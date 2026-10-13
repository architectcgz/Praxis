package session

import (
	"context"
	"fmt"

	"praxis/internal/contracts"
	contextmodel "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
)

// RebuildTaskContext 以冻结的可见性标识重建历史，再追加本 Task 已提交的消息。
func (s *Service) RebuildTaskContext(ctx context.Context, task taskmodel.Task) (contextmodel.BuildResult, error) {
	var result contextmodel.BuildResult
	err := s.tx.InTx(ctx, func(txCtx context.Context) error {
		stream, err := s.messages.LoadMessages(txCtx, task.SessionID, task.AgentID, 0)
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
		allEntries, err := s.loadContextEntries(txCtx, task.SessionID)
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
		result, err = s.contextBuilder.Build(contextmodel.BuildInput{
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
