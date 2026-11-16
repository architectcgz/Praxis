package session

import (
	"context"
	modelusage "praxis/internal/core/model/usage"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
)

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
