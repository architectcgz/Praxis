package jsonl

import (
	"context"
	"errors"
	messagemodel "praxis/internal/core/message"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// AppendAssistant 原子保存消息及最终输出证据，不创建工具执行实体。
func (w MessageWriter) AppendAssistant(ctx context.Context, value repository.AssistantWrite) (stored messagemodel.Message, inserted bool, err error) {
	message := value.Message
	if ctx == nil || message.Role != messagemodel.RoleAssistant || message.AuthorKind != messagemodel.AuthorAgent || message.AuthorID != w.owner.ID.String() || message.TurnID == "" {
		return stored, false, errors.New("assistant ownership is required")
	}
	err = w.streams.s.InTx(ctx, func(txCtx context.Context) error {
		execution, err := (AgentExecutionRepository{w.streams.s}).Get(txCtx, identity.AgentExecutionID(message.ExecutionID))
		if err != nil {
			return err
		}
		if execution.SessionID != w.owner.SessionID || execution.AgentID != w.owner.ID || message.RequestID != execution.RequestID.String() {
			return fault.ErrRequestConflict
		}
		if value.FinalOutput != nil {
			if value.FinalOutput.AssistantID != message.ID || value.FinalOutput.StopReason != message.StopReason || message.StopReason != "completed" && message.StopReason != "stop_sequence" {
				return fault.ErrRequestConflict
			}
		}
		stored, inserted, err = w.streams.s.appendMessage(txCtx, w.owner, message)
		if err != nil {
			return err
		}
		if !inserted {
			if value.FinalOutput != nil && (execution.Progress.FinalAssistantID != stored.ID || execution.Progress.FinalStopReason != stored.StopReason) {
				return fault.ErrRequestConflict
			}
			return nil
		}
		if !execution.Active() {
			return fault.ErrRequestConflict
		}
		if value.FinalOutput != nil {
			if err := execution.RecordFinalOutput(*value.FinalOutput); err != nil {
				return err
			}
		}
		calls := 0
		for _, block := range stored.Blocks {
			if block.Kind == messagemodel.BlockToolCall {
				calls++
			}
		}
		execution.Progress.ToolCalls += calls
		if value.FinalOutput != nil || calls > 0 {
			return (AgentExecutionRepository{w.streams.s}).Save(txCtx, execution)
		}
		return nil
	})
	if err != nil {
		return messagemodel.Message{}, false, err
	}
	return stored, inserted, nil
}
