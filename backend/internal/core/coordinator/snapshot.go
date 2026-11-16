package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	modelusage "praxis/internal/core/model/usage"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

func (s *Coordinator) ListAgentEntries(ctx context.Context, id identity.SessionID, agentID identity.AgentID, head, before string, limit int) (sessionmodel.HistoryPage, error) {
	owner, err := s.GetSession(ctx, id)
	if err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	return owner.AgentPage(ctx, agentID, head, before, limit)
}

type SessionContent struct {
	Agents []sessionmodel.AgentView
	Page   sessionmodel.HistoryPage
}

func (s *Coordinator) SessionContent(ctx context.Context, id identity.SessionID) (SessionContent, error) {
	owner, err := s.GetSession(ctx, id)
	if err != nil {
		return SessionContent{}, err
	}
	info, err := s.sessionRecords.Get(ctx, id)
	if err != nil {
		return SessionContent{}, err
	}
	definition, err := owner.Definition(agentmodel.DefinitionPrimary)
	if err != nil {
		return SessionContent{}, err
	}
	agents, err := owner.Agents(ctx, info, definition, 0)
	if err != nil {
		return SessionContent{}, err
	}
	for i := range agents {
		s.mu.Lock()
		active := s.runs[runKey{agents[i].ID}]
		var run *agentrun.Run
		if active != nil {
			run = active.run
		}
		s.mu.Unlock()
		if run != nil {
			agents[i].State = run.State()
		}
	}
	page, err := owner.AgentPage(ctx, info.MainAgentID(), "", "", 50)
	return SessionContent{Agents: agents, Page: page}, err
}
func (s *Coordinator) ListSessionUsage(ctx context.Context, id identity.SessionID, ids []identity.TurnID) ([]modelusage.UsageRecord, error) {
	owner, err := s.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return owner.UsageRecords(ctx, ids)
}
func (s *Coordinator) GetSessionUsageSummary(ctx context.Context, id identity.SessionID) (sessionmodel.SessionUsageSummary, error) {
	owner, err := s.GetSession(ctx, id)
	if err != nil {
		return sessionmodel.SessionUsageSummary{}, err
	}
	totals, requests := owner.UsageTotals()
	summary := sessionmodel.SessionUsageSummary{Totals: totals, TotalRequests: requests, CacheReadComplete: totals.CacheReadReports == requests}
	if summary.CacheReadComplete && totals.InputTokens > 0 {
		summary.CacheReadRatio = new(float64(totals.CacheReadInputTokens) / float64(totals.InputTokens))
	}
	return summary, nil
}
