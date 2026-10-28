package storage

import (
	"cmp"
	"context"
	"fmt"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

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
func (r AgentExecutionRepository) AppendTiming(ctx context.Context, value timing.Record) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	execution, ok := r.storage.state.executions[identity.AgentExecutionID(value.ExecutionID)]
	if !ok {
		return errorcode.ErrNotFound
	}
	kind := "agent_execution.timing_finished"
	if value.Status == timing.Running {
		kind = "agent_execution.timing_started"
	}
	value = cloneTiming(value)
	key := timingKey(value.ExecutionID, value.ID)
	if err := r.storage.appendEventLocked(ctx, execution.SessionID, kind, "agent_execution_timing", key, value); err != nil {
		return err
	}
	r.storage.state.timings[key] = value
	return nil
}
func (r AgentExecutionRepository) ListTimings(ctx context.Context, id identity.AgentExecutionID) ([]timing.Record, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return nil, err
	}
	defer r.storage.mu.RUnlock()
	if _, ok := r.storage.state.executions[id]; !ok {
		return nil, errorcode.ErrNotFound
	}
	result := make([]timing.Record, 0)
	for _, value := range r.storage.state.timings {
		if value.ExecutionID == id.String() {
			result = append(result, cloneTiming(value))
		}
	}
	return sortTimings(result), nil
}
func (r AgentExecutionRepository) ListAgentTimings(ctx context.Context, id identity.AgentID, limit int) ([]timing.Record, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return nil, err
	}
	defer r.storage.mu.RUnlock()
	result := make([]timing.Record, 0)
	for _, value := range r.storage.state.timings {
		if execution, ok := r.storage.state.executions[identity.AgentExecutionID(value.ExecutionID)]; ok && execution.AgentID == id {
			result = append(result, cloneTiming(value))
		}
	}
	return capped(sortTimings(result), limit), nil
}
func sortTimings(values []timing.Record) []timing.Record {
	return sorted(values, func(a, b timing.Record) int {
		return cmp.Or(b.StartedAt.Compare(a.StartedAt), cmp.Compare(b.ID, a.ID))
	})
}
