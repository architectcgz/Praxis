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
	if s.data.ExecutionIndex(identity.AgentExecutionID(value.ExecutionID)) < 0 {
		return errorcode.ErrNotFound
	}
	kind := EventTimingFinished
	if value.Status == timing.Running {
		kind = EventTimingStarted
	}
	s.data.SetTiming(value)
	return s.appendEvent(ctx, kind, "agent_execution_timing", value.ExecutionID+":"+value.ID, value)
}

func (s *Session) ListAgentTimingRecords(agentID identity.AgentID, branchID identity.BranchID, limit int) []timing.Record {
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]timing.Record, 0)
	for _, value := range s.data.Timings {
		index := s.data.ExecutionIndex(identity.AgentExecutionID(value.ExecutionID))
		if index >= 0 && s.data.Executions[index].AgentID == agentID && s.data.Executions[index].BranchID == branchID {
			records = append(records, value)
		}
	}
	return capped(sorted(records, func(a, b timing.Record) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	}), limit)
}
