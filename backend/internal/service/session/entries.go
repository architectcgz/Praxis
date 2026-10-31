package session

import (
	"context"
	"errors"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

var _ sessionmodel.History = (*Service)(nil)

// AppendEntry 在会话锁内检查准入和工具配对，追加完整记录后更新业务内存态。
func (s *Service) AppendEntry(ctx context.Context, sessionID identity.SessionID, data sessionentry.ContextEntry) (sessionentry.ContextEntry, bool, error) {
	h, err := s.lockHistory(ctx, sessionID)
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	defer h.mu.Unlock()
	if err := h.validateEntryLocked(data); err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	stored, inserted, err := h.appendEntryLocked(ctx, entryRecord{
		AgentID: h.state.session.MainAgentID(),
		Data:    data,
	})
	if err != nil {
		return sessionentry.ContextEntry{}, false, err
	}
	// 任务输入等准入成功再追加；摘要通过完整压缩结果替换 Context。
	if inserted && stored.SourceTaskID == "" && !stored.IsSummary() && h.context != nil {
		h.context.Append(stored)
	}
	return stored, inserted, nil
}

// GetEntry 包含尚未完成执行准入的输入，供准入重试确认消息身份。
func (s *Service) GetEntry(ctx context.Context, sessionID identity.SessionID, id string) (sessionentry.ContextEntry, error) {
	h, err := s.lockHistory(ctx, sessionID)
	if err != nil {
		return sessionentry.ContextEntry{}, err
	}
	defer h.mu.Unlock()
	return h.entryLocked(id)
}

// ListEntries 返回主会话的完整已准入历史和同一次读取的事件边界。
func (s *Service) ListEntries(ctx context.Context, sessionID identity.SessionID) (sessionentry.List, error) {
	h, err := s.lockHistory(ctx, sessionID)
	if err != nil {
		return sessionentry.List{}, err
	}
	defer h.mu.Unlock()
	return h.listEntriesLocked(false), nil
}

func (h *sessionHistory) validateEntryLocked(data sessionentry.ContextEntry) error {
	session := h.state.session
	if session.State != sessionmodel.SessionActive || data.ExecutionID == "" && (data.Role != role.User || data.SourceTaskID != "" || data.IsSummary()) {
		return errorcode.ErrRequestConflict
	}
	if err := data.ValidateContent(); err != nil {
		return err
	}
	if data.Role == role.Assistant && (data.AuthorKind != sessionentry.AuthorAgent || data.AuthorID != session.MainAgentID().String()) {
		return errors.New("assistant author does not match the session main agent")
	}
	if data.ExecutionID != "" {
		index := h.state.executionIndex(identity.AgentExecutionID(data.ExecutionID))
		preparing := index < 0 && data.Role == role.User && !data.IsSummary() && data.ID == "input:"+data.RequestID
		if !preparing {
			if index < 0 {
				return errorcode.ErrNotFound
			}
			execution := h.state.executions[index]
			if execution.AgentID != session.MainAgentID() || execution.RequestID.String() != data.RequestID {
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
	page := h.listEntriesLocked(true)
	if data.IsSummary() {
		if data.AuthorID != session.MainAgentID().String() || data.ExecutionID == "" || data.RequestID == "" {
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
					declaration, declaredName = new(entry), prior.Name
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
