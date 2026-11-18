package session

import (
	"context"
	"fmt"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (s *Session) appendEntry(ctx context.Context, agentID identity.AgentID, value sessionentry.Entry, terminal bool) (*sessionentry.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	s.mu.RLock()
	exists := s.data.HasAgent(agentID)
	s.mu.RUnlock()
	if !exists {
		return nil, false, errorcode.ErrNotFound
	}
	if value.RunID == "" || value.ID == "" {
		return nil, false, errorcode.ErrInvalidValue
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = s.clock.Now()
	}
	stored := new(value)
	if terminal {
		if err := s.log.CommitTerminal(ctx, agentID, stored); err != nil {
			return nil, false, err
		}
	} else if err := s.log.Append(ctx, agentID, stored); err != nil {
		return nil, false, err
	}
	s.mu.Lock()
	s.data.Accept(agentID, stored)
	s.mu.Unlock()
	return stored, true, nil
}

// CommitTerminal 提交 Run 的最后一条 Entry 并建立 durability boundary。
func (s *Session) CommitTerminal(ctx context.Context, agentID identity.AgentID, value sessionentry.Entry) (*sessionentry.Entry, error) {
	stored, _, err := s.appendEntry(ctx, agentID, value, true)
	return stored, err
}

func (s *Session) Entry(ctx context.Context, id string) (*sessionentry.Entry, error) {
	return s.findEntry(ctx, s.data.MainAgentID(), id)
}

func (s *Session) findEntry(ctx context.Context, agentID identity.AgentID, id string) (*sessionentry.Entry, error) {
	var found *sessionentry.Entry
	err := s.walkHistory(ctx, agentID, func(value *sessionentry.Entry) bool {
		if value.ID == id {
			found = value
			return false
		}
		return true
	})
	if err != nil {
		return nil, err
	}
	if found == nil {
		return nil, errorcode.ErrNotFound
	}
	return found, nil
}

// walkHistory 从新到旧逐页访问事实，调用方可在取得所需信息后停止。
func (s *Session) walkHistory(ctx context.Context, agentID identity.AgentID, visit func(*sessionentry.Entry) bool) error {
	s.mu.RLock()
	exists := s.data.HasAgent(agentID)
	s.mu.RUnlock()
	if !exists {
		return errorcode.ErrNotFound
	}
	var cursor *int64
	for {
		page, err := s.log.ReadBackward(ctx, agentID, cursor, 200)
		if err != nil {
			return err
		}
		for index := len(page.Entries) - 1; index >= 0; index-- {
			if !visit(&page.Entries[index]) {
				return nil
			}
		}
		if !page.HasMore {
			return nil
		}
		cursor = page.NextCursor
	}
}

// HistoryPage 表示一个 Agent 的完整 History 页面，Entry 顺序为旧到新。
type HistoryPage struct {
	Entries    []sessionentry.Entry
	NextCursor *int64
	HasMore    bool
}

func (s *Session) AgentPage(ctx context.Context, agentID identity.AgentID, cursor *int64, limit int) (HistoryPage, error) {
	s.mu.RLock()
	exists := s.data.HasAgent(agentID)
	s.mu.RUnlock()
	if !exists {
		return HistoryPage{}, errorcode.ErrNotFound
	}
	page, err := s.log.ReadBackward(ctx, agentID, cursor, limit)
	if err != nil {
		return HistoryPage{}, fmt.Errorf("read agent history: %w", err)
	}
	return page, nil
}
