package jsonl

import (
	"context"
	"errors"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// AppendAssistant 原子提交调用方已构建的消息、工具身份及最终输出证据。
func (w MessageWriter) AppendAssistant(ctx context.Context, value repository.AssistantWrite) (stored messagemodel.Message, inserted bool, err error) {
	message := value.Message
	if ctx == nil || message.Role != messagemodel.RoleAssistant || message.AuthorKind != messagemodel.AuthorAgent || message.AuthorID != w.owner.ID.String() || message.TurnID == "" {
		return stored, false, errors.New("assistant ownership is required")
	}
	err = w.streams.s.InTx(ctx, func(txCtx context.Context) error {
		task, err := (AgentExecutionRepository{w.streams.s}).Get(txCtx, identity.AgentExecutionID(message.ExecutionID))
		if err != nil {
			return err
		}
		if !task.Active() || task.SessionID != w.owner.SessionID || task.AgentID != w.owner.ID || message.RequestID != task.RequestID.String() {
			return fault.ErrRequestConflict
		}
		calls := make(map[string]toolexecution.Execution, len(value.ToolCalls))
		for _, call := range value.ToolCalls {
			if _, ok := calls[call.ProviderToolCallID]; ok {
				return fault.ErrRequestConflict
			}
			if call.ExecutionID != task.ID || call.SessionID != task.SessionID || call.AgentID != task.AgentID || call.TurnID.String() != message.TurnID || call.ArgumentsDigest != toolexecution.ArgumentsDigest(call.Name, call.Arguments) || call.Status != toolexecution.StatusRequested || call.BudgetReserved || len(call.NormalizedArguments) != 0 || call.Path != "" {
				return fault.ErrRequestConflict
			}
			calls[call.ProviderToolCallID] = call
		}
		for _, block := range message.Blocks {
			if block.Kind != messagemodel.BlockToolCall {
				continue
			}
			call, ok := calls[block.CallID]
			if !ok || string(call.Name) != block.Name || call.ArgumentsDigest != toolexecution.ArgumentsDigest(call.Name, block.Input) {
				return fault.ErrRequestConflict
			}
			delete(calls, block.CallID)
		}
		if len(calls) != 0 {
			return fault.ErrRequestConflict
		}
		if value.FinalOutput != nil {
			if value.FinalOutput.AssistantID != message.ID || value.FinalOutput.StopReason != message.StopReason {
				return fault.ErrRequestConflict
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
			existing, err := (ToolExecutionRepository{w.streams.s}).FindByExecutionCall(txCtx, task.ID, call.ProviderToolCallID)
			if err == nil {
				if existing.Name != call.Name || existing.ArgumentsDigest != call.ArgumentsDigest || existing.TurnID != call.TurnID || existing.SessionID != call.SessionID || existing.AgentID != call.AgentID {
					return fault.ErrRequestConflict
				}
				continue
			}
			if !errors.Is(err, fault.ErrNotFound) {
				return err
			}
			if !inserted {
				return fault.ErrRequestConflict
			}
			if err := (ToolExecutionRepository{w.streams.s}).Save(txCtx, call); err != nil {
				return err
			}
		}
		if !inserted {
			current, err := (AgentExecutionRepository{w.streams.s}).Get(txCtx, task.ID)
			if err != nil {
				return err
			}
			if current.Progress != task.Progress {
				return fault.ErrRequestConflict
			}
			return nil
		}
		if value.FinalOutput != nil {
			if value.FinalOutput.StopReason != "completed" && value.FinalOutput.StopReason != "stop_sequence" {
				return fault.InvalidValue("task.finalOutput", "invalid final output evidence")
			}
			return (AgentExecutionRepository{w.streams.s}).Save(txCtx, task)
		}
		return nil
	})
	if err != nil {
		return messagemodel.Message{}, false, err
	}
	return stored, inserted, nil
}
