package session

import (
	"cmp"
	"context"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (s *Service) AppendTiming(ctx context.Context, sessionID identity.SessionID, value timing.Record) error {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return err
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if err := owner.checkState(ctx); err != nil {
		return err
	}
	if owner.data.ExecutionIndex(identity.AgentExecutionID(value.ExecutionID)) < 0 {
		return errorcode.ErrNotFound
	}
	kind := "agent_execution.timing_finished"
	if value.Status == timing.Running {
		kind = "agent_execution.timing_started"
	}
	value = cloneTiming(value)
	if err := owner.appendEvent(ctx, kind, "agent_execution_timing", timingKey(value.ExecutionID, value.ID), value); err != nil {
		return err
	}
	owner.data.SetTiming(value)
	return nil
}

func (s *Service) ListAgentTimingRecords(ctx context.Context, agentID identity.AgentID, branchID identity.BranchID, limit int) (records []timing.Record, err error) {
	owner, err := s.OpenedSession(ctx, identity.SessionID(agentID))
	if err != nil {
		return nil, err
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return nil, err
	}
	records = make([]timing.Record, 0)
	for _, value := range owner.data.Timings {
		index := owner.data.ExecutionIndex(identity.AgentExecutionID(value.ExecutionID))
		if index >= 0 && owner.data.Executions[index].AgentID == agentID && owner.data.Executions[index].BranchID == branchID {
			records = append(records, cloneTiming(value))
		}
	}
	return capped(sorted(records, func(a, b timing.Record) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	}), limit), nil
}

func timingKey(executionID, operationID string) string {
	return executionID + ":" + operationID
}

func cloneTiming(value timing.Record) timing.Record {
	if value.DurationMS != nil {
		value.DurationMS = new(*value.DurationMS)
	}
	if value.FirstResponseMS != nil {
		value.FirstResponseMS = new(*value.FirstResponseMS)
	}
	return value
}
