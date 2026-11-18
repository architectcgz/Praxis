package session

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/identity"
)

func (s *Session) CreateSubagent(ctx context.Context, agentID identity.AgentID) error {
	if err := s.log.CreateSubagent(ctx, agentID); err != nil {
		return err
	}
	s.mu.Lock()
	s.data.AgentContexts[agentID] = AgentContext{}
	s.mu.Unlock()
	return nil
}

// AgentHistory 将历史读写和终态提交绑定到同一个 Agent。
type AgentHistory struct {
	session *Session
	agentID identity.AgentID
}

func (s *Session) BindHistory(agentID identity.AgentID) AgentHistory {
	return AgentHistory{
		session: s,
		agentID: agentID,
	}
}

func (h AgentHistory) SessionID() identity.SessionID { return h.session.data.ID }
func (h AgentHistory) AgentID() identity.AgentID     { return h.agentID }

func (h AgentHistory) Append(ctx context.Context, value sessionentry.Entry) (*sessionentry.Entry, bool, error) {
	return h.session.appendEntry(ctx, h.agentID, value, false)
}

func (h AgentHistory) ModelEntries(ctx context.Context) ([]*sessionentry.Entry, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return h.session.AgentContext(h.agentID)
}

func (h AgentHistory) CommitTerminal(ctx context.Context, value sessionentry.Entry) (*sessionentry.Entry, error) {
	return h.session.CommitTerminal(ctx, h.agentID, value)
}
