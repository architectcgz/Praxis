package toolexecution

import (
	"context"
	"errors"
	"praxis/internal/core/message"
	"praxis/internal/fault"
	agentservice "praxis/internal/service/agent"
	sessionservice "praxis/internal/service/session"
)

// commitResult 只保存消息回执；输出字节从回执派生，重复请求不会重复累计。
func (s *Service) commitResult(ctx context.Context, call message.ToolCall, data message.Message) (stored message.Message, inserted bool, err error) {
	if data.Role != message.RoleTool || len(data.Blocks) != 1 || data.Blocks[0].Kind != message.BlockToolResult || data.AuthorKind != message.AuthorTool || data.AuthorID != data.Blocks[0].Name {
		return stored, false, errors.New("tool receipt must contain a single owned result")
	}
	err = func(txCtx context.Context) error {
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
		return err
	}(ctx)
	if err != nil {
		return message.Message{}, false, err
	}
	return stored, inserted, nil
}
