package coordinator

import (
	"context"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
)

func (s *Coordinator) ListAgentEntries(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, branchID identity.BranchID, head, before string, limit int) (sessionmodel.HistoryPage, error) {
	if err := s.enter(); err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, sessionID)
	if err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	defer unlock()
	return s.sessions.ListAgentEntries(ctx, sessionID, agentID, branchID, head, before, limit)
}

type SessionContent struct {
	Agents []sessionservice.AgentView
	Page   sessionmodel.HistoryPage
}

func (s *Coordinator) SessionContent(ctx context.Context, id identity.SessionID, branchID identity.BranchID) (SessionContent, error) {
	if err := s.enter(); err != nil {
		return SessionContent{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, id)
	if err != nil {
		return SessionContent{}, err
	}
	defer unlock()
	agents, err := s.sessions.ListSessionAgents(ctx, id, branchID, 0)
	if err != nil {
		return SessionContent{}, err
	}
	page, err := s.sessions.ListAgentEntries(ctx, id, identity.AgentID(id), branchID, "", "", 50)
	return SessionContent{Agents: agents, Page: page}, err
}

func (s *Coordinator) ListSessionUsage(ctx context.Context, sessionID identity.SessionID, ids []identity.TurnID) ([]providerapi.UsageRecord, error) {
	if err := s.enter(); err != nil {
		return nil, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	return s.sessions.ListSessionUsage(ctx, sessionID, ids)
}

func (s *Coordinator) GetSessionUsageSummary(ctx context.Context, sessionID identity.SessionID) (sessionservice.SessionUsageSummary, error) {
	if err := s.enter(); err != nil {
		return sessionservice.SessionUsageSummary{}, err
	}
	defer s.running.Done()
	unlock, err := s.lockSession(ctx, sessionID)
	if err != nil {
		return sessionservice.SessionUsageSummary{}, err
	}
	defer unlock()
	return s.sessions.GetSessionUsageSummary(ctx, sessionID)
}
