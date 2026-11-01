package session

import (
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

// SessionUsageSummary 汇总主、子 Execution 的累计值；请求明细只用于逐次展示。
// CacheReadRatio 使用 0 到 1，零输入或存在未知缓存计数时为 nil。
type SessionUsageSummary struct {
	agentexecution.UsageTotals
	Records           []providerapi.UsageRecord
	CacheReadRatio    *float64
	CacheReadComplete bool
}

func (s *Service) ListSessionUsage(ctx context.Context, sessionID identity.SessionID) ([]providerapi.UsageRecord, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	records, _ := owner.UsageSnapshot()
	return records, nil
}

func (s *Service) GetSessionUsageSummary(ctx context.Context, sessionID identity.SessionID) (SessionUsageSummary, error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return SessionUsageSummary{}, err
	}
	records, totals := owner.UsageSnapshot()
	summary := SessionUsageSummary{
		UsageTotals:       totals,
		Records:           records,
		CacheReadComplete: totals.CacheReadReports == totals.ReportedRequests,
	}
	if summary.CacheReadComplete && totals.InputTokens > 0 {
		summary.CacheReadRatio = new(float64(totals.CacheReadInputTokens) / float64(totals.InputTokens))
	}
	return summary, nil
}
