package session

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	modelconfig "praxis/internal/core/model/config"
	modelusage "praxis/internal/core/model/usage"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

func (s *Service) Configuration(ctx context.Context, id identity.SessionID) (*sessionmodel.Configuration, error) {
	owner, err := s.core.GetSession(ctx, id)
	if err != nil {
		return nil, err
	}
	return owner.Configuration(), nil
}

func (s *Service) ModelOptions(ctx context.Context, id identity.SessionID) ([]modelconfig.Option, error) {
	config, err := s.Configuration(ctx, id)
	if err != nil {
		return nil, err
	}
	return config.Models.ListModels(), nil
}

func (s *Service) SelectedModel(ctx context.Context, id identity.SessionID) (sessionmodel.ModelChoice, error) {
	owner, err := s.core.GetSession(ctx, id)
	if err != nil {
		return sessionmodel.ModelChoice{}, err
	}
	return owner.Choice(), nil
}

func (s *Service) ListAgentEntries(ctx context.Context, sessionID identity.SessionID, agentID identity.AgentID, head, before string, limit int) (sessionmodel.HistoryPage, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return sessionmodel.HistoryPage{}, err
	}
	return owner.AgentPage(ctx, agentID, head, before, limit)
}

func (s *Service) Entry(ctx context.Context, sessionID identity.SessionID, id string) (*sessionentry.ContextEntry, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.Entry(ctx, id)
}

func (s *Service) GetRun(ctx context.Context, sessionID identity.SessionID, id identity.RunID) (record sessionentry.RunRecord, err error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return record, err
	}
	return owner.GetRun(ctx, id)
}

func (s *Service) FindRunByTask(ctx context.Context, agentID identity.AgentID, taskID identity.TaskID) (record sessionentry.RunRecord, err error) {
	owner, err := s.OpenedSession(ctx, identity.SessionID(agentID))
	if err != nil {
		return record, err
	}
	return owner.FindRunByTask(ctx, agentID, taskID)
}

func (s *Service) ListSessionRuns(ctx context.Context, sessionID identity.SessionID) ([]sessionentry.HistorySummary, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.ListSessionRuns(ctx)
}

func (s *Service) ListSessionUsage(ctx context.Context, sessionID identity.SessionID, turnIDs []identity.TurnID) ([]modelusage.UsageRecord, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.UsageRecords(ctx, turnIDs)
}

func (s *Service) GetSessionUsageSummary(ctx context.Context, sessionID identity.SessionID) (sessionmodel.SessionUsageSummary, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return sessionmodel.SessionUsageSummary{}, err
	}
	totals, requests := owner.UsageTotals()
	summary := sessionmodel.SessionUsageSummary{
		Totals:            totals,
		TotalRequests:     requests,
		CacheReadComplete: totals.CacheReadReports == requests,
	}
	if summary.CacheReadComplete && totals.InputTokens > 0 {
		summary.CacheReadRatio = new(float64(totals.CacheReadInputTokens) / float64(totals.InputTokens))
	}
	return summary, nil
}
