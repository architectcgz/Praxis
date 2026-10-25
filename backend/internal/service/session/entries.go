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
func (s *Service) AppendEntry(ctx context.Context, sessionID identity.SessionID, data sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	if err := RequireWritable(ctx, s.sessions, sessionID); err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	owner, err := s.mainAgent(ctx, sessionID)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	if data.Role != role.User || data.ExecutionID != "" || data.SourceTaskID != "" {
		return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
	}
	return s.entries.Append(ctx, sessionentry.Record{
		SessionID: owner.SessionID,
		AgentID:   owner.ID,
		Data:      data,
	})
}

// ListEntries 按会话保存的 MainAgentID 读取唯一历史，不创建实例或聚合 Subagent 的私有消息。
func (s *Service) ListEntries(ctx context.Context, sessionID identity.SessionID) (repository.EntryList, error) {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return repository.EntryList{}, err
	}
	return s.entries.List(ctx, repository.EntryQuery{AgentID: session.MainAgentID})
}
