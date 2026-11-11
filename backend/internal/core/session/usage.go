package session

import (
	"cmp"
	"context"
	modelusage "praxis/internal/core/model/usage"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

func (s *Session) UsageTotals() (modelusage.Totals, uint64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.data.Usage, s.data.TotalRequests
}

// UsageRecords 仅查询当前展示涉及的 Turn，内存接纳值替换同一请求的磁盘投影。
func (s *Session) UsageRecords(ctx context.Context, turnIDs []identity.TurnID) ([]providerapi.UsageRecord, error) {
	s.commitMu.Lock()
	defer s.commitMu.Unlock()
	if err := s.syncHistory(ctx); err != nil {
		return nil, err
	}
	requests, err := s.log.Requests(ctx, turnIDs)
	if err != nil {
		return nil, err
	}
	byID := make(map[identity.ModelRequestID]providerapi.UsageRecord)
	wanted := make(map[identity.TurnID]bool, len(turnIDs))
	for _, id := range turnIDs {
		wanted[id] = true
	}
	s.mu.RLock()
	for _, request := range s.data.ModelRequests {
		if wanted[request.TurnID] {
			requests = append(requests, request)
		}
	}
	for _, request := range requests {
		if request.Usage == nil {
			delete(byID, request.ID)
			continue
		}
		owner := s.data.Runs[s.data.RunIndex(request.RunID)]
		byID[request.ID] = providerapi.UsageRecord{
			SessionID: owner.SessionID.String(),
			AgentID:   owner.AgentID.String(),
			RunID:     owner.ID.String(),
			RequestID: request.ID,
			TurnID:    request.TurnID,
			Usage:     *request.Usage,
		}
	}
	s.mu.RUnlock()
	records := make([]providerapi.UsageRecord, 0, len(byID))
	for _, record := range byID {
		records = append(records, record)
	}
	return sorted(records, func(a, b providerapi.UsageRecord) int { return cmp.Compare(a.RequestID, b.RequestID) }), nil
}
