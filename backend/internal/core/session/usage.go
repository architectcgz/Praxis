package session

import (
	"cmp"
	"context"
	sessionentry "praxis/internal/core/entry"
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/identity"
)

func (s *Session) UsageTotals(ctx context.Context) (modelusage.Totals, uint64, error) {
	var totals modelusage.Totals
	var requests uint64
	for _, agentID := range s.agentIDs() {
		err := s.walkHistory(ctx, agentID, func(value *sessionentry.Entry) bool {
			if _, ok := value.Payload.(sessionentry.ModelRequestStarted); ok {
				requests++
			}
			if request, _ := value.RequestFact(); request != nil && request.Status != sessionentry.RequestRunning && request.Usage != nil {
				u := request.Usage
				totals.Add(u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens)
			}
			return true
		})
		if err != nil {
			return modelusage.Totals{}, 0, err
		}
	}
	return totals, requests, nil
}

// UsageRecords 从请求终态事实提取展示用量，不依赖完整执行记录或长期请求索引。
func (s *Session) UsageRecords(ctx context.Context, turnIDs []identity.TurnID) ([]modelusage.UsageRecord, error) {
	wanted := make(map[identity.TurnID]bool, len(turnIDs))
	for _, id := range turnIDs {
		wanted[id] = true
	}
	records := make([]modelusage.UsageRecord, 0)
	if len(wanted) == 0 {
		return records, nil
	}
	for _, agentID := range s.agentIDs() {
		err := s.walkHistory(ctx, agentID, func(value *sessionentry.Entry) bool {
			request, _ := value.RequestFact()
			if request != nil && request.Status != sessionentry.RequestRunning && wanted[request.TurnID] && request.Usage != nil {
				records = append(records, modelusage.UsageRecord{
					SessionID: s.data.ID.String(),
					AgentID:   agentID.String(),
					RunID:     request.RunID.String(),
					RequestID: request.ID,
					TurnID:    request.TurnID,
					Usage:     *request.Usage,
				})
			}
			return true
		})
		if err != nil {
			return nil, err
		}
	}
	return sorted(records, func(a, b modelusage.UsageRecord) int { return cmp.Compare(a.RequestID, b.RequestID) }), nil
}

// SessionUsageSummary 汇总主、子执行的请求用量；请求明细只用于逐次展示。
// CacheReadRatio 使用 0 到 1，零输入或存在未知缓存计数时为 nil。
type SessionUsageSummary struct {
	modelusage.Totals
	TotalRequests     uint64
	CacheReadRatio    *float64
	CacheReadComplete bool
}
