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

func (s *Session) AppendSubagentEntry(ctx context.Context, agentID identity.AgentID, value sessionentry.Entry) (*sessionentry.Entry, bool, error) {
	return s.appendEntry(ctx, agentID, value, false)
}

func (s *Session) BindHistory(agentID identity.AgentID) sessionentry.AppendEntry {
	if agentID == s.data.MainAgentID() {
		return s.AppendEntry
	}
	return func(ctx context.Context, value sessionentry.Entry) (*sessionentry.Entry, bool, error) {
		return s.AppendSubagentEntry(ctx, agentID, value)
	}
}
