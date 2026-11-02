package session

import (
	"cmp"
	"context"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (s *Session) AppendTiming(ctx context.Context, value timing.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.data.ExecutionIndex(identity.AgentExecutionID(value.ExecutionID))
	if index < 0 {
		return errorcode.ErrNotFound
	}
	kind := EventTimingFinished
	if value.Status == timing.Running {
		kind = EventTimingStarted
	}
	s.data.SetTiming(value)
	return s.log.AppendEvent(ctx, s.data.ID, Event{
		Type:      kind,
		Key:       value.ExecutionID + ":" + value.ID,
		AgentID:   s.data.Executions[index].AgentID,
		CreatedAt: s.clock.Now(),
		Payload:   value,
	})
}

func (s *Session) ListAgentTimings(agentID identity.AgentID, branchID identity.BranchID, limit int) []timing.View {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]timing.View, 0, len(s.data.Executions)+len(s.data.Timings))
	for _, execution := range s.data.Executions {
		if execution.AgentID != agentID || execution.BranchID != branchID {
			continue
		}
		if value := execution.TimingView(); value != nil {
			values = append(values, *value)
		}
	}
	for _, value := range s.data.Timings {
		index := s.data.ExecutionIndex(identity.AgentExecutionID(value.ExecutionID))
		if index < 0 {
			continue
		}
		execution := s.data.Executions[index]
		if execution.AgentID == agentID && execution.BranchID == branchID {
			values = append(values, value.View(execution.SessionID.String(), execution.AgentID.String(), execution.Active()))
		}
	}
	return capped(sorted(values, func(a, b timing.View) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	}), limit)
}
