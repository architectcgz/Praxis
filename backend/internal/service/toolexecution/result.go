package toolexecution

import (
	"context"
	"errors"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/fault"
	agentservice "praxis/internal/service/agent"
	sessionservice "praxis/internal/service/session"
)

// commitResult 只保存消息回执；输出字节从回执派生，重复请求不会重复累计。
func (s *Service) commitResult(ctx context.Context, call sessionentry.ToolCall, data sessionentry.ContextEntry) (stored sessionentry.ContextEntry, inserted bool, err error) {
	if data.Role != role.Tool || len(data.Content) != 1 || data.Content[0].Kind != sessionentry.BlockToolResult || data.AuthorKind != sessionentry.AuthorTool || data.AuthorID != data.Content[0].Name {
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
		if execution.AgentID != call.AgentID || execution.SessionID != call.SessionID || execution.RequestID.String() != call.RequestID || data.ExecutionID != execution.ID.String() || data.RequestID != call.RequestID || data.TurnID != call.TurnID.String() || data.Content[0].CallID != call.CallID || data.Content[0].Name != string(call.Name) {
			return fault.ErrRequestConflict
		}
		owner, err := s.deps.Agents.Get(txCtx, call.AgentID)
		if err != nil {
			return err
		}
		if owner.SessionID != call.SessionID {
			return fault.ErrRequestConflict
		}
		stored, inserted, err = s.deps.Entries.Append(txCtx, sessionentry.Record{Stream: agentservice.EntryStream(owner), AgentID: owner.ID, Data: data})
		return err
	}(ctx)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	return stored, inserted, nil
}
