package toolexecution

import (
	"context"
	"errors"
	"praxis/internal/core/message"
	"praxis/internal/fault"
	agentservice "praxis/internal/service/agent"
	sessionservice "praxis/internal/service/session"
)

// commitResult 将消息回执与输出字节放在同一事务；重复回执不再次累计。
func (s *Service) commitResult(ctx context.Context, call message.ToolCall, data message.Message) (stored message.Message, inserted bool, err error) {
	if data.Role != message.RoleTool || len(data.Blocks) != 1 || data.Blocks[0].Kind != message.BlockToolResult || data.AuthorKind != message.AuthorTool || data.AuthorID != data.Blocks[0].Name {
		return stored, false, errors.New("tool receipt must contain a single owned result")
	}
	err = s.deps.Transactions.InTx(ctx, func(txCtx context.Context) error {
		if err := sessionservice.RequireWritable(txCtx, s.deps.Sessions, call.SessionID); err != nil {
			return err
		}
		execution, err := s.deps.Tasks.Get(txCtx, call.ExecutionID)
		if err != nil {
			return err
		}
		if execution.AgentID != call.AgentID || execution.SessionID != call.SessionID || execution.RequestID.String() != call.RequestID || data.ExecutionID != execution.ID.String() || data.RequestID != call.RequestID || data.TurnID != call.TurnID.String() || data.Blocks[0].CallID != call.CallID || data.Blocks[0].Name != string(call.Name) {
			return fault.ErrRequestConflict
		}
		owner, err := s.deps.Agents.Get(txCtx, call.AgentID)
		if err != nil {
			return err
		}
		if owner.SessionID != call.SessionID {
			return fault.ErrRequestConflict
		}
		stored, inserted, err = s.deps.Messages.Append(txCtx, message.Record{Stream: agentservice.MessageStream(owner), AgentID: owner.ID, Data: data})
		if err != nil || !inserted {
			return err
		}
		execution.Progress.OutputBytes += int64(len(stored.Blocks[0].Text))
		return s.deps.Tasks.Save(txCtx, execution)
	})
	if err != nil {
		return message.Message{}, false, err
	}
	return stored, inserted, nil
}
