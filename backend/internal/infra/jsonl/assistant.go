package jsonl

import (
	"context"
	"errors"
	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	toolmodel "praxis/internal/core/tool_execution"
	"praxis/internal/repository"
)

// AppendAssistant 原子提交调用方已构建的消息、工具身份及最终输出证据。
func (w MessageWriter) AppendAssistant(ctx context.Context, value repository.AssistantWrite) (stored sessionmodel.MessageData, inserted bool, err error) {
	message := value.Message
	if ctx == nil || message.Role != sessionmodel.RoleAssistant || message.AuthorKind != sessionmodel.AuthorAgent || message.AuthorID != w.owner.ID.String() || message.TurnID == "" {
		return stored, false, errors.New("assistant ownership is required")
	}
	err = w.streams.s.InTx(ctx, func(txCtx context.Context) error {
		task, err := (TaskRepository{w.streams.s}).Get(txCtx, contracts.TaskID(message.TaskID))
		if err != nil {
			return err
		}
		if !task.Active() || task.SessionID != w.owner.SessionID || task.AgentID != w.owner.ID || message.RequestID != task.RequestID.String() {
			return contracts.ErrRequestConflict
		}
		calls := make(map[string]toolmodel.Execution, len(value.ToolCalls))
		for _, call := range value.ToolCalls {
			if _, ok := calls[call.ProviderToolCallID]; ok {
				return contracts.ErrRequestConflict
			}
			if call.TaskID != task.ID || call.SessionID != task.SessionID || call.AgentID != task.AgentID || call.TurnID.String() != message.TurnID || call.ArgumentsDigest != toolmodel.ArgumentsDigest(call.Name, call.Arguments) || call.Status != toolmodel.StatusRequested || call.BudgetReserved || len(call.NormalizedArguments) != 0 || call.Path != "" {
				return contracts.ErrRequestConflict
			}
			calls[call.ProviderToolCallID] = call
		}
		for _, block := range message.Blocks {
			if block.Kind != sessionmodel.BlockToolCall {
				continue
			}
			call, ok := calls[block.CallID]
			if !ok || string(call.Name) != block.Name || call.ArgumentsDigest != toolmodel.ArgumentsDigest(call.Name, block.Input) {
				return contracts.ErrRequestConflict
			}
			delete(calls, block.CallID)
		}
		if len(calls) != 0 {
			return contracts.ErrRequestConflict
		}
		if value.FinalOutput != nil {
			if value.FinalOutput.AssistantID != message.ID || value.FinalOutput.StopReason != message.StopReason {
				return contracts.ErrRequestConflict
			}
			if err := task.RecordFinalOutput(*value.FinalOutput); err != nil {
				return err
			}
		}
		stored, inserted, err = w.streams.s.appendMessage(txCtx, w.owner, message)
		if err != nil {
			return err
		}
		for _, call := range value.ToolCalls {
			existing, err := (ToolExecutionRepository{w.streams.s}).FindByTaskCall(txCtx, task.ID, call.ProviderToolCallID)
			if err == nil {
				if existing.Name != call.Name || existing.ArgumentsDigest != call.ArgumentsDigest || existing.TurnID != call.TurnID || existing.SessionID != call.SessionID || existing.AgentID != call.AgentID {
					return contracts.ErrRequestConflict
				}
				continue
			}
			if !errors.Is(err, contracts.ErrNotFound) {
				return err
			}
			if !inserted {
				return contracts.ErrRequestConflict
			}
			if err := (ToolExecutionRepository{w.streams.s}).Save(txCtx, call); err != nil {
				return err
			}
		}
		if !inserted {
			current, err := (TaskRepository{w.streams.s}).Get(txCtx, task.ID)
			if err != nil {
				return err
			}
			if current.Progress != task.Progress {
				return contracts.ErrRequestConflict
			}
			return nil
		}
		if value.FinalOutput != nil {
			if value.FinalOutput.StopReason != "completed" && value.FinalOutput.StopReason != "stop_sequence" {
				return contracts.InvalidValue("task.finalOutput", "invalid final output evidence")
			}
			return (TaskRepository{w.streams.s}).Save(txCtx, task)
		}
		return nil
	})
	if err != nil {
		return sessionmodel.MessageData{}, false, err
	}
	return stored, inserted, nil
}
