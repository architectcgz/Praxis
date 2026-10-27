package session

import (
	"context"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

var _ sessionmodel.History = (*Service)(nil)

// AppendEntry 将输入、模型回答、工具回执或压缩摘要追加到主会话历史。
// 执行归属、工具配对和摘要来源由存储在同一接纳边界内校验；子执行不能写入。
func (s *Service) AppendEntry(ctx context.Context, sessionID identity.SessionID, data sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	if session.State != sessionmodel.SessionActive {
		return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
	}
	if data.ExecutionID == "" && (data.Role != role.User || data.SourceTaskID != "" || data.IsSummary()) {
		return sessionentry.ContextEntry{}, false, errorcode.ErrRequestConflict
	}
	stored, inserted, err := s.entries.Append(ctx, sessionentry.Record{
		SessionID: session.ID,
		AgentID:   session.MainAgentID,
		Data:      data,
	})
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	// 任务输入等准入成功再追加；摘要等完整压缩结果保存后通过 ReplaceContext 替换。
	if inserted && stored.SourceTaskID == "" && !stored.IsSummary() {
		if value := s.contexts[sessionID]; value != nil {
			value.Append(stored)
		}
	}
	return stored, inserted, nil
}

// UpdateToolResult 先更新主会话历史，再同步当前上下文；存储失败时保留原回执。
func (s *Service) UpdateToolResult(ctx context.Context, sessionID identity.SessionID, id, text string) (sessionentry.ContextEntry, error) {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return sessionentry.ContextEntry{}, err
	}
	if session.State != sessionmodel.SessionActive {
		return sessionentry.ContextEntry{}, errorcode.ErrRequestConflict
	}
	stored, err := s.entries.UpdateToolResult(ctx, session.ID, session.MainAgentID, id, text)
	if err != nil {
		return sessionentry.ContextEntry{}, err
	}
	if value := s.contexts[sessionID]; value != nil {
		value.UpdateToolResult(id, text)
	}
	return stored, nil
}

// GetEntry 包含尚未完成执行准入的输入，供准入重试确认已接纳的消息身份。
func (s *Service) GetEntry(ctx context.Context, sessionID identity.SessionID, id string) (sessionentry.ContextEntry, error) {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return sessionentry.ContextEntry{}, err
	}
	return s.entries.Get(ctx, session.MainAgentID, id)
}

// ListEntries 读取主会话的完整已准入历史，不聚合子执行的私有消息。
func (s *Service) ListEntries(ctx context.Context, sessionID identity.SessionID) (sessionentry.List, error) {
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return sessionentry.List{}, err
	}
	return s.entries.List(ctx, sessionentry.Query{AgentID: session.MainAgentID})
}
