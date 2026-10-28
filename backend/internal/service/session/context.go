package session

import (
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

// ContextEntries 返回会话已维护的上下文，只在首次使用时从保存结果和后续条目恢复。
func (s *Service) ContextEntries(ctx context.Context, sessionID identity.SessionID) ([]sessionentry.ContextEntry, error) {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	value := s.contexts[sessionID]
	if value == nil {
		entries, err := s.entries.LoadContext(ctx, session.ID, session.MainAgentID)
		if err != nil {
			return nil, err
		}
		value = appcontext.NewContext(entries)
		s.contexts[sessionID] = value
	}
	return value.Entries(), nil
}

// ReplaceContext 先保存完整压缩结果，再直接替换内存条目；失败时保留当前上下文。
func (s *Service) ReplaceContext(ctx context.Context, sessionID identity.SessionID, entries []sessionentry.ContextEntry) error {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	session, err := s.sessions.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if session.State != sessionmodel.SessionActive {
		return errorcode.ErrRequestConflict
	}
	if len(entries) == 0 {
		return errors.New("saved context entries are required")
	}
	for _, entry := range entries {
		if err := entry.Validate(); err != nil {
			return err
		}
		prior, err := s.entries.Get(ctx, session.MainAgentID, entry.ID)
		if err != nil {
			return err
		}
		if entry.Sequence != prior.Sequence || entry.ExecutionID != prior.ExecutionID || entry.RequestID != prior.RequestID || entry.Role != prior.Role {
			return errorcode.ErrRequestConflict
		}
	}
	if err := s.entries.SaveContext(ctx, session.ID, session.MainAgentID, entries); err != nil {
		return err
	}
	value := s.contexts[sessionID]
	if value == nil {
		value = appcontext.NewContext(nil)
		s.contexts[sessionID] = value
	}
	value.Replace(entries)
	return nil
}

// AdmitInput 在 Execution 已接纳后才将任务输入加入上下文。
// 用户消息先追加、Execution 后保存之间若失败，未准入输入不能提前影响下一次模型请求。
func (s *Service) AdmitInput(ctx context.Context, sessionID identity.SessionID, id string) error {
	s.contextMu.Lock()
	defer s.contextMu.Unlock()
	entry, err := s.GetEntry(ctx, sessionID, id)
	if err != nil {
		return err
	}
	if entry.Role != role.User || entry.SourceTaskID == "" || entry.ExecutionID == "" {
		return errorcode.ErrRequestConflict
	}
	if value := s.contexts[sessionID]; value != nil {
		value.Append(entry)
	}
	return nil
}
