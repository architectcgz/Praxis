package session

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (s *Session) CreateSubagent(ctx context.Context, branchID identity.BranchID, record agentrun.RunRecord, input sessionentry.ContextEntry) (*sessionentry.ContextEntry, error) {
	s.commitMu.Lock()
	s.mu.RLock()
	_, exists := s.data.AgentHistories[record.AgentID]
	s.mu.RUnlock()
	if !exists {
		if err := s.log.CreateSubagent(ctx, branchID, record.AgentID); err != nil {
			s.commitMu.Unlock()
			return nil, err
		}
		s.mu.Lock()
		s.data.AgentHistories[record.AgentID] = AgentHistory{BranchID: branchID}
		s.mu.Unlock()
	}
	s.commitMu.Unlock()
	stored, _, err := s.AppendSubagentEntry(ctx, record.AgentID, input)
	if err != nil {
		return stored, err
	}
	_, err = s.SaveRun(ctx, record)
	return stored, err
}

func (s *Session) AppendSubagentEntry(ctx context.Context, agentID identity.AgentID, value sessionentry.ContextEntry) (*sessionentry.ContextEntry, bool, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	s.mu.RLock()
	history, exists := s.data.AgentHistories[agentID]
	s.mu.RUnlock()
	if !exists {
		return nil, false, errorcode.ErrNotFound
	}
	return s.appendEntry(ctx, history.BranchID, agentID, value)
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
	return s.contextEntries(ctx, history.HeadEntryID)
}
