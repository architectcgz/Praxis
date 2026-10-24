package session

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// AppendEntry 将会话输入直接追加到 Main Agent，不维护另一份 Session 消息或上下文。
// 绑定 Execution 的输入和输出由执行用例提交。
func (s *Service) AppendEntry(ctx context.Context, sessionID identity.SessionID, data sessionentry.ContextEntry) (stored sessionentry.ContextEntry, inserted bool, err error) {
	err = func(txCtx context.Context) error {
		if err := RequireWritable(txCtx, s.sessions, sessionID); err != nil {
			return err
		}
		owner, err := s.agents.GetMainBySession(txCtx, sessionID)
		if err != nil {
			return err
		}
		if data.Role != role.User || data.ExecutionID != "" || data.SourceTaskID != "" {
			return errorcode.ErrRequestConflict
		}
		stored, inserted, err = s.entries.Append(txCtx, sessionentry.Record{AgentID: owner.ID, Data: data})
		return err
	}(ctx)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	return stored, inserted, nil
}

// ListEntries 直接读取 Main Agent 的历史；Session 不聚合 Subagent 的私有消息。
func (s *Service) ListEntries(ctx context.Context, sessionID identity.SessionID) (page repository.EntryList, err error) {
	err = func(txCtx context.Context) error {
		if _, err := s.sessions.Get(txCtx, sessionID); err != nil {
			return err
		}
		owner, err := s.agents.GetMainBySession(txCtx, sessionID)
		if err != nil {
			return err
		}
		page, err = s.entries.List(txCtx, repository.EntryQuery{AgentID: owner.ID})
		return err
	}(ctx)
	return page, err
}
