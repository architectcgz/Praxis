package session

import (
	"cmp"
	agentrun "praxis/internal/core/agent/run"
	providerapi "praxis/internal/core/provider"
)

// UsageSnapshot 在同一读锁下取得请求明细和各 Run 的最新累计值。
func (s *Session) UsageSnapshot() ([]providerapi.UsageRecord, agentrun.UsageTotals) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]providerapi.UsageRecord, 0)
	var totals agentrun.UsageTotals
	for _, owner := range s.data.Runs {
		totals.Merge(owner.Progress.Usage)
	}
	for _, request := range s.data.ModelRequests {
		if request.Usage == nil {
			continue
		}
		owner := s.data.Runs[s.data.RunIndex(request.RunID)]
		records = append(records, providerapi.UsageRecord{
			SessionID: owner.SessionID.String(),
			AgentID:   owner.AgentID.String(),
			RunID:     owner.ID.String(),
			TurnID:    request.TurnID,
			Usage:     request.Usage.Clone(),
		})
	}
	return sorted(records, func(a, b providerapi.UsageRecord) int { return cmp.Compare(a.TurnID, b.TurnID) }), totals
}
