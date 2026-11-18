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

func (s *Service) Entry(ctx context.Context, sessionID identity.SessionID, id string) (*sessionentry.Entry, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.Entry(ctx, id)
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
	totals, requests, err := owner.UsageTotals(ctx)
	if err != nil {
		return sessionmodel.SessionUsageSummary{}, err
	}
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
