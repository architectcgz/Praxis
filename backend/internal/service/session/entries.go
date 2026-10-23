package session

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// AppendEntry 追加共享输入；绑定 Execution 的输入和输出由执行用例原子提交。
func (s *Service) AppendEntry(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, data sessionentry.ContextEntry) (stored sessionentry.ContextEntry, inserted bool, err error) {
	err = func(txCtx context.Context) error {
		if err := RequireWritable(txCtx, s.sessions, sessionID); err != nil {
			return err
		}
		owner, err := s.agents.Get(txCtx, agentID)
		if err != nil {
			return err
		}
		if owner.SessionID != sessionID || !owner.CanReadSessionContext() || data.Role != role.User || data.ExecutionID != "" || data.SourceTaskID != "" {
			return errorcode.ErrRequestConflict
		}
		stored, inserted, err = s.entries.Append(txCtx, sessionentry.Record{Stream: sessionentry.StreamKey{SessionID: sessionID}, AgentID: agentID, Data: data})
		return err
	}(ctx)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	return stored, inserted, nil
}

// ListEntries 只读取共享流，Session 全量消息索引不作为可见历史。
func (s *Service) ListEntries(ctx context.Context, sessionID identity.SessionID) (page repository.EntryList, err error) {
	err = func(txCtx context.Context) error {
		if _, err := s.sessions.Get(txCtx, sessionID); err != nil {
			return err
		}
		var err error
		page, err = s.entries.List(txCtx, repository.EntryQuery{Stream: sessionentry.StreamKey{SessionID: sessionID}})
		return err
	}(ctx)
	return page, err
}
