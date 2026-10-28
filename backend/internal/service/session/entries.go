package session

import (
	"context"
	"errors"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

var _ sessionmodel.History = (*Service)(nil)

// AppendEntry 将输入、模型回答、工具回执或压缩摘要追加到主会话历史。
// 执行归属、工具配对和摘要来源在会话锁内检查；仓储只保存已经准入的事实。
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
	if err := s.validateEntry(ctx, session, data); err != nil {
		return sessionentry.ContextEntry{}, false, err
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
	prior, err := s.entries.Get(ctx, session.MainAgentID, id)
	if err != nil {
		return sessionentry.ContextEntry{}, err
	}
	if prior.Role != role.Tool || len(prior.Content) != 1 || prior.Content[0].Kind != sessionentry.BlockToolResult {
		return sessionentry.ContextEntry{}, errorcode.ErrRequestConflict
	}
	if len(text) > tool.MaxInlineResultBytes {
		return sessionentry.ContextEntry{}, errorcode.ErrInvalidRequest
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

func (s *Service) validateEntry(ctx context.Context, session sessionmodel.Session, data sessionentry.ContextEntry) error {
	if err := data.ValidateContent(); err != nil {
		return err
	}
	if data.Role == role.Assistant && (data.AuthorKind != sessionentry.AuthorAgent || data.AuthorID != session.MainAgentID.String()) {
		return errors.New("assistant author does not match the session main agent")
	}
	if data.ExecutionID != "" {
		execution, err := s.executions.Get(ctx, identity.AgentExecutionID(data.ExecutionID))
		preparing := errors.Is(err, errorcode.ErrNotFound) && data.Role == role.User && !data.IsSummary() && data.ID == "input:"+data.RequestID
		if !preparing {
			if err != nil {
				return err
			}
			if execution.SessionID != session.ID || execution.AgentID != session.MainAgentID || execution.RequestID.String() != data.RequestID {
				return errorcode.ErrRequestConflict
			}
		}
	}
	needsHistory := data.IsSummary()
	for _, block := range data.Content {
		if block.Kind == sessionentry.BlockToolCall || block.Kind == sessionentry.BlockToolResult {
			needsHistory = true
		}
	}
	if !needsHistory {
		return nil
	}
	page, err := s.entries.List(ctx, sessionentry.Query{AgentID: session.MainAgentID, IncludeUnadmitted: true})
	if err != nil {
		return err
	}
	if data.IsSummary() {
		if data.AuthorID != session.MainAgentID.String() || data.ExecutionID == "" || data.RequestID == "" {
			return errors.New("summary ownership is required")
		}
		seen := make(map[string]bool)
		for _, id := range data.Replaces {
			found := false
			for _, entry := range page.Entries {
				if entry.ID == id && id != data.ID {
					found = true
					break
				}
			}
			if !found || seen[id] {
				return errors.New("summary source is missing or duplicated")
			}
			seen[id] = true
		}
	}
	for _, block := range data.Content {
		if block.Kind != sessionentry.BlockToolCall && block.Kind != sessionentry.BlockToolResult {
			continue
		}
		var declaration *sessionentry.ContextEntry
		var declaredName string
		for _, entry := range page.Entries {
			if entry.ExecutionID != data.ExecutionID {
				continue
			}
			for _, prior := range entry.Content {
				if prior.CallID != block.CallID {
					continue
				}
				if prior.Kind == sessionentry.BlockToolCall {
					if block.Kind == sessionentry.BlockToolCall && entry.ID != data.ID {
						return errorcode.ErrRequestConflict
					}
					copy := entry
					declaration, declaredName = &copy, prior.Name
				}
				if block.Kind == sessionentry.BlockToolResult && prior.Kind == sessionentry.BlockToolResult && entry.ID != data.ID {
					return errorcode.ErrRequestConflict
				}
			}
		}
		if block.Kind == sessionentry.BlockToolResult {
			if declaration == nil {
				return fmt.Errorf("tool call %q: %w", block.CallID, errorcode.ErrNotFound)
			}
			if declaration.RequestID != data.RequestID || declaration.TurnID != data.TurnID || declaredName != block.Name {
				return errors.New("tool receipt ownership does not match its declaration")
			}
		}
	}
	return nil
}
