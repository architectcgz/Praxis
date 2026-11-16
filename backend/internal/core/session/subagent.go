package session

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (s *Session) CreateSubagent(ctx context.Context, agentID identity.AgentID) error {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	if err := s.log.CreateSubagent(ctx, agentID); err != nil {
		return err
	}
	s.mu.Lock()
	s.data.AgentHistories[agentID] = AgentHistory{}
	s.mu.Unlock()
	return nil
}

func (s *Session) AppendSubagentEntry(ctx context.Context, agentID identity.AgentID, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	_, exists := s.data.AgentHistories[agentID]
	s.mu.RUnlock()
	if !exists {
		return nil, false, errorcode.ErrNotFound
	}
	return s.appendEntry(ctx, agentID, value)
}

func (s *Session) SubagentEntries(ctx context.Context, agentID identity.AgentID) ([]*sessionentry.ContextEntry, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	history, exists := s.data.AgentHistories[agentID]
	s.mu.RUnlock()
	if !exists {
		return nil, errorcode.ErrNotFound
	}
	return s.contextEntries(ctx, history.HeadEntryID, "")
}

func (s *Session) BindHistory(agentID identity.AgentID) sessionentry.AppendEntry {
	if agentID == s.data.MainAgentID() {
		return s.AppendEntry
	}
	return func(ctx context.Context, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
		return s.AppendSubagentEntry(ctx, agentID, value)
	}
}
