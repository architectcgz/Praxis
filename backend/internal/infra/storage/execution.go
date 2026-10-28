package storage

import (
	"cmp"
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

type AgentExecutionRepository struct{ storage *Store }

func cloneExecution(value agentexecution.ExecutionRecord) agentexecution.ExecutionRecord {
	value.Config = value.Config.Clone()
	return value
}

func (r AgentExecutionRepository) Get(ctx context.Context, sessionID identity.SessionID, id identity.AgentExecutionID) (agentexecution.ExecutionRecord, error) {
	if err := r.storage.lockSession(ctx, sessionID); err != nil {
		return agentexecution.ExecutionRecord{}, err
	}
	defer r.storage.mu.Unlock()
	value, ok := r.storage.state.executions[id]
	if !ok || value.SessionID != sessionID {
		return agentexecution.ExecutionRecord{}, errorcode.ErrNotFound
	}
	return cloneExecution(value), nil
}
func (r AgentExecutionRepository) Save(ctx context.Context, value agentexecution.ExecutionRecord) error {
	if err := r.storage.lockSession(ctx, value.SessionID); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	value = cloneExecution(value)
	// 派生展示字段不属于持久化记录，也不留在仓储状态中。
	value.Progress.OutputBytes, value.Progress.ToolCalls = 0, 0
	value.Progress.FinalAssistantID, value.Progress.FinalStopReason = "", ""
	if err := r.storage.appendEventLocked(ctx, value.SessionID, "agent_execution.saved", "agent_execution", value.ID.String(), value); err != nil {
		return err
	}
	r.storage.state.executions[value.ID] = value
	return nil
}
func (r AgentExecutionRepository) FindByRequest(ctx context.Context, id identity.AgentID, request identity.RequestID) (agentexecution.ExecutionRecord, error) {
	return r.find(ctx, identity.SessionID(id), func(value agentexecution.ExecutionRecord) bool {
		return value.AgentID == id && value.RequestID == request
	})
}
func (r AgentExecutionRepository) GetActiveByAgent(ctx context.Context, id identity.AgentID) (agentexecution.ExecutionRecord, error) {
	return r.find(ctx, identity.SessionID(id), func(value agentexecution.ExecutionRecord) bool { return value.AgentID == id && value.Active() })
}
func (r AgentExecutionRepository) find(ctx context.Context, sessionID identity.SessionID, match func(agentexecution.ExecutionRecord) bool) (agentexecution.ExecutionRecord, error) {
	values, err := r.list(ctx, sessionID, match)
	if err != nil {
		return agentexecution.ExecutionRecord{}, err
	}
	if len(values) == 0 {
		return agentexecution.ExecutionRecord{}, errorcode.ErrNotFound
	}
	if len(values) != 1 {
		return agentexecution.ExecutionRecord{}, errorcode.ErrRequestConflict
	}
	return values[0], nil
}
func (r AgentExecutionRepository) list(ctx context.Context, sessionID identity.SessionID, match func(agentexecution.ExecutionRecord) bool) ([]agentexecution.ExecutionRecord, error) {
	if err := r.storage.lockSession(ctx, sessionID); err != nil {
		if errors.Is(err, errorcode.ErrNotFound) {
			return []agentexecution.ExecutionRecord{}, nil
		}
		return nil, err
	}
	defer r.storage.mu.Unlock()
	values := make([]agentexecution.ExecutionRecord, 0)
	for _, value := range r.storage.state.executions {
		if value.SessionID == sessionID && match(value) {
			values = append(values, cloneExecution(value))
		}
	}
	return values, nil
}
func (r AgentExecutionRepository) ListBySession(ctx context.Context, id identity.SessionID) ([]agentexecution.ExecutionRecord, error) {
	values, err := r.list(ctx, id, func(agentexecution.ExecutionRecord) bool { return true })
	return sorted(values, executionOrder), err
}
func (r AgentExecutionRepository) ListByAgent(ctx context.Context, id identity.AgentID, limit int) ([]agentexecution.ExecutionRecord, error) {
	values, err := r.list(ctx, identity.SessionID(id), func(value agentexecution.ExecutionRecord) bool { return value.AgentID == id })
	return capped(sorted(values, func(a, b agentexecution.ExecutionRecord) int { return -executionOrder(a, b) }), limit), err
}
func executionOrder(a, b agentexecution.ExecutionRecord) int {
	return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
}
