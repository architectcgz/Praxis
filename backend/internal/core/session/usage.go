package session

import (
	"context"
	sessionentry "praxis/internal/core/entry"
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/identity"
)

// UsageSnapshot 是各 Agent 已计入的请求用量与对应的日志前缀。
type UsageSnapshot struct {
	modelusage.Totals
	TotalRequests uint64
	HistoryEnds   map[identity.AgentID]int64
}

func (s *Session) UsageSnapshot(ctx context.Context) (UsageSnapshot, error) {
	snapshot := UsageSnapshot{HistoryEnds: make(map[identity.AgentID]int64)}
	for _, agentID := range s.agentIDs() {
		var end int64
		err := s.walkHistory(ctx, agentID, func(value *sessionentry.Entry) bool {
			end = max(end, value.HistoryEnd)
			request, _ := value.RequestFact()
			if request == nil {
				return true
			}
			if request.Status == sessionentry.RequestRunning {
				snapshot.TotalRequests++
			} else if request.Usage != nil {
				snapshot.Add(*request.Usage)
			}
			return true
		})
		if err != nil {
			return UsageSnapshot{}, err
		}
		snapshot.HistoryEnds[agentID] = end
	}
	return snapshot, nil
}
