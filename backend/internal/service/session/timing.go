package session

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (s *Service) AppendTiming(ctx context.Context, sessionID identity.SessionID, value timing.Record) error {
	h, err := s.lockHistory(ctx, sessionID)
	if err != nil {
		return err
	}
	defer h.mu.Unlock()
	if h.state.executionIndex(identity.AgentExecutionID(value.ExecutionID)) < 0 {
		return errorcode.ErrNotFound
	}
	kind := "agent_execution.timing_finished"
	if value.Status == timing.Running {
		kind = "agent_execution.timing_started"
	}
	value = cloneTiming(value)
	if err := h.appendEventLocked(ctx, kind, "agent_execution_timing", timingKey(value.ExecutionID, value.ID), value); err != nil {
		return err
	}
	h.state.setTiming(value)
	return nil
}

func (s *Service) ListAgentTimingRecords(ctx context.Context, agentID identity.AgentID, limit int) ([]timing.Record, error) {
	h, err := s.lockHistory(ctx, identity.SessionID(agentID))
	if err != nil {
		if errors.Is(err, errorcode.ErrNotFound) {
			return []timing.Record{}, nil
		}
		return nil, err
	}
	defer h.mu.Unlock()
	result := make([]timing.Record, 0)
	for _, value := range h.state.timings {
		index := h.state.executionIndex(identity.AgentExecutionID(value.ExecutionID))
		if index >= 0 && h.state.executions[index].AgentID == agentID {
			result = append(result, cloneTiming(value))
		}
	}
	return capped(sorted(result, func(a, b timing.Record) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	}), limit), nil
}

func timingKey(executionID, operationID string) string {
	return fmt.Sprintf("%d:%s%s", len(executionID), executionID, operationID)
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
