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
	"slices"
)

// ContextEntries 首次使用时从保存结果和后续条目恢复，之后直接读取会话持有的 Context。
func (s *Service) ContextEntries(ctx context.Context, sessionID identity.SessionID) ([]sessionentry.ContextEntry, error) {
	h, err := s.lockHistory(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer h.mu.Unlock()
	if h.context == nil {
		entries := h.restoreContextLocked()
		if err := s.interruptPreviousExecutions(ctx, h); err != nil {
			return nil, err
		}
		h.context = appcontext.NewContext(entries)
	}
	return h.context.Entries(), nil
}

// interruptPreviousExecutions 在首次初始化 Context 时收尾旧执行，不创建 Run 或请求模型。
// 调用方持有会话锁；新执行必须在 Context 初始化成功后才准入。
func (s *Service) interruptPreviousExecutions(ctx context.Context, h *sessionHistory) error {
	for _, record := range sorted(slices.Clone(h.state.executions), executionOrder) {
		if !record.Active() {
			continue
		}
		at := s.clock.Now()
		if record.Status == agentexecution.ExecutionStarting {
			record.Start(at)
		}
		record.End(agentexecution.ExecutionInterrupted, "", at)
		if err := h.saveExecutionLocked(ctx, record); err != nil {
			return err
		}
	}
	return nil
}

// ReplaceContext 先追加完整压缩结果，再替换内存条目；写入失败保留当前上下文。
func (s *Service) ReplaceContext(ctx context.Context, sessionID identity.SessionID, entries []sessionentry.ContextEntry) error {
	h, err := s.lockHistory(ctx, sessionID)
	if err != nil {
		return err
	}
	defer h.mu.Unlock()
	if h.state.session.State != sessionmodel.SessionActive {
		return errorcode.ErrRequestConflict
	}
	if len(entries) == 0 {
		return errors.New("saved context entries are required")
	}
	for _, entry := range entries {
		if err := entry.Validate(); err != nil {
			return err
		}
		prior, err := h.entryLocked(entry.ID)
		if err != nil {
			return err
		}
		if entry.Sequence != prior.Sequence || entry.ExecutionID != prior.ExecutionID || entry.RequestID != prior.RequestID || entry.Role != prior.Role {
			return errorcode.ErrRequestConflict
		}
	}
	value := cloneContext(contextRecord{
		AgentID:         h.state.session.MainAgentID(),
		ThroughSequence: h.state.sequence,
		Entries:         entries,
	})
	if err := h.appendEventLocked(ctx, "context.saved", "context", sessionID.String(), value); err != nil {
		return err
	}
	h.state.context = &value
	if h.context == nil {
		h.context = appcontext.NewContext(nil)
	}
	h.context.Replace(entries)
	return nil
}

// AdmitInput 在 Execution 已接纳后才将任务输入加入上下文。
func (s *Service) AdmitInput(ctx context.Context, sessionID identity.SessionID, id string) error {
	h, err := s.lockHistory(ctx, sessionID)
	if err != nil {
		return err
	}
	defer h.mu.Unlock()
	entry, err := h.entryLocked(id)
	if err != nil {
		return err
	}
	if entry.Role != role.User || entry.SourceTaskID == "" || entry.ExecutionID == "" {
		return errorcode.ErrRequestConflict
	}
	if h.context != nil {
		h.context.Append(entry)
	}
	return nil
}

func (h *sessionHistory) restoreContextLocked() []sessionentry.ContextEntry {
	var entries []sessionentry.ContextEntry
	var boundary uint64
	if value := h.state.context; value != nil {
		cloned := cloneContext(*value)
		entries, boundary = cloned.Entries, cloned.ThroughSequence
	}
	for _, stored := range h.state.entries {
		value := stored.Record.Data
		if stored.Record.AgentID != h.state.session.MainAgentID() || value.IsSummary() {
			continue
		}
		if value.Sequence <= boundary {
			if stored.UpdatedSequence > boundary {
				for index, entry := range entries {
					if entry.ID == value.ID {
						entries[index].Content = value.Clone().Content
						break
					}
				}
			}
			continue
		}
		if value.Role == role.User && value.ExecutionID != "" && h.state.executionIndex(identity.AgentExecutionID(value.ExecutionID)) < 0 {
			continue
		}
		entries = append(entries, value.Clone())
	}
	return entries
}
