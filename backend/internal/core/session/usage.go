package session

import (
	"cmp"
	agentexecution "praxis/internal/core/agent/execution"
	providerapi "praxis/internal/core/provider"
)

// UsageSnapshot 在同一读锁下取得请求明细和各 Execution 的最新累计值。
func (s *Session) UsageSnapshot() ([]providerapi.UsageRecord, agentexecution.UsageTotals) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]providerapi.UsageRecord, 0)
	var totals agentexecution.UsageTotals
	for _, execution := range s.data.Executions {
		totals.Merge(execution.Progress.Usage)
	}
	for _, request := range s.data.ModelRequests {
		if request.Usage == nil {
			continue
		}
		execution := s.data.Executions[s.data.ExecutionIndex(request.ExecutionID)]
		records = append(records, providerapi.UsageRecord{
			SessionID:   execution.SessionID.String(),
			AgentID:     execution.AgentID.String(),
			ExecutionID: execution.ID.String(),
			TurnID:      request.TurnID,
			Usage:       request.Usage.Clone(),
		})
	}
	return sorted(records, func(a, b providerapi.UsageRecord) int { return cmp.Compare(a.TurnID, b.TurnID) }), totals
}
