package session

import (
	"context"
	"fmt"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"time"
)

func (s *Session) appendEntry(ctx context.Context, agentID identity.AgentID, value entry.Entry, terminal bool) (*entry.Entry, bool, error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	agentCtx := s.agentContext(agentID)
	if agentCtx == nil {
		return nil, false, errorcode.ErrNotFound
	}
	if value.RunID == "" || value.ID == "" {
		return nil, false, errorcode.ErrInvalidValue
	}
	if value.CreatedAt.IsZero() {
		value.CreatedAt = time.Now().UTC()
	}
	stored := new(value)
	if terminal {
		if err := s.log.CommitTerminal(ctx, agentID, stored); err != nil {
			return nil, false, err
		}
	} else if err := s.log.Append(ctx, agentID, stored); err != nil {
		return nil, false, err
	}
	agentCtx.Accept(stored)
	return stored, true, nil
}

// HistoryPage 表示一个 Agent 的完整 History 页面，Entry 顺序为旧到新。
type HistoryPage struct {
	Entries    []entry.Entry
	NextCursor *int64
	HasMore    bool
}

func (s *Session) AgentPage(ctx context.Context, agentID identity.AgentID, cursor *int64, limit int) (HistoryPage, error) {
	if s.agentContext(agentID) == nil {
		return HistoryPage{}, errorcode.ErrNotFound
	}
	page, err := s.log.ReadBackward(ctx, agentID, cursor, limit)
	if err != nil {
		return HistoryPage{}, fmt.Errorf("read agent history: %w", err)
	}
	return page, nil
}
