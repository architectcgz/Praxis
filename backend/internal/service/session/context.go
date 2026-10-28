package session

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
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
		entries, err := s.entries.LoadContext(ctx, session.ID, session.MainAgentID())
		if err != nil {
			return nil, err
		}
		if err := s.interruptPreviousExecutions(ctx, sessionID); err != nil {
			return nil, err
		}
		value = appcontext.NewContext(entries)
		s.contexts[sessionID] = value
	}
	return value.Entries(), nil
}

// interruptPreviousExecutions 只在首次载入 Context 时收尾旧进程留下的执行，不创建 Run 或请求模型。
// 调用方持有 contextMu；新的 Execution 必须在 Context 初始化成功后才准入。
func (s *Service) interruptPreviousExecutions(ctx context.Context, sessionID identity.SessionID) error {
	records, err := s.executions.ListBySession(ctx, sessionID)
	if err != nil {
		return err
	}
	for _, record := range records {
		if !record.Active() {
			continue
		}
		at := s.clock.Now()
		if record.Status == agentexecution.ExecutionStarting {
			// 与正常执行的启动前结算约定一致，起止时间均记录为本次收尾时间。
			record.Start(at)
		}
		record.End(agentexecution.ExecutionInterrupted, "", at)
		if err := s.executions.Save(ctx, record); err != nil {
			return err
		}
	}
	return nil
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
		prior, err := s.entries.Get(ctx, session.MainAgentID(), entry.ID)
		if err != nil {
			return err
		}
		if entry.Sequence != prior.Sequence || entry.ExecutionID != prior.ExecutionID || entry.RequestID != prior.RequestID || entry.Role != prior.Role {
			return errorcode.ErrRequestConflict
		}
	}
	if err := s.entries.SaveContext(ctx, session.ID, session.MainAgentID(), entries); err != nil {
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
