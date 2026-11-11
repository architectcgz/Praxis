package session

import (
	"context"
	modelusage "praxis/internal/core/model/usage"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

// SessionUsageSummary 汇总主、子 Run 的累计值；请求明细只用于逐次展示。
// CacheReadRatio 使用 0 到 1，零输入或存在未知缓存计数时为 nil。
type SessionUsageSummary struct {
	modelusage.Totals
	TotalRequests     uint64
	CacheReadRatio    *float64
	CacheReadComplete bool
}

func (s *Service) ListSessionUsage(ctx context.Context, sessionID identity.SessionID, turnIDs []identity.TurnID) ([]providerapi.UsageRecord, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return owner.UsageRecords(ctx, turnIDs)
}

func (s *Service) GetSessionUsageSummary(ctx context.Context, sessionID identity.SessionID) (SessionUsageSummary, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return SessionUsageSummary{}, err
	}
	totals, requests := owner.UsageTotals()
	summary := SessionUsageSummary{
		Totals:            totals,
		TotalRequests:     requests,
		CacheReadComplete: totals.CacheReadReports == requests,
	}
	if summary.CacheReadComplete && totals.InputTokens > 0 {
		summary.CacheReadRatio = new(float64(totals.CacheReadInputTokens) / float64(totals.InputTokens))
	}
	return summary, nil
}
