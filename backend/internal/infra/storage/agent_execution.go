package storage

import (
	"cmp"
	"context"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

type AgentExecutionRepository struct{ storage *Storage }

func (r AgentExecutionRepository) Get(ctx context.Context, id identity.AgentExecutionID) (agentexecution.Execution, error) {
	return get[agentexecution.Execution](ctx, r.storage, "agent_execution", id.String())
}

func (r AgentExecutionRepository) Save(ctx context.Context, value agentexecution.Execution) error {
	return save(ctx, r.storage, value.SessionID.String(), "agent_execution", value.ID.String(), "agent_execution.saved", value)
}

func (r AgentExecutionRepository) FindByRequest(ctx context.Context, id identity.AgentID, request identity.RequestID) (agentexecution.Execution, error) {
	return r.find(ctx, func(value agentexecution.Execution) bool { return value.AgentID == id && value.RequestID == request })
}

func (r AgentExecutionRepository) GetActiveByAgent(ctx context.Context, id identity.AgentID) (agentexecution.Execution, error) {
	return r.find(ctx, func(value agentexecution.Execution) bool { return value.AgentID == id && value.Active() })
}

func (r AgentExecutionRepository) find(ctx context.Context, match func(agentexecution.Execution) bool) (result agentexecution.Execution, err error) {
	values, err := list[agentexecution.Execution](ctx, r.storage, "agent_execution", match)
	if err != nil {
		return result, err
	}
	if len(values) == 0 {
		return result, errorcode.ErrNotFound
	}
	if len(values) != 1 {
		return result, errorcode.ErrRequestConflict
	}
	return values[0], nil
}

func (r AgentExecutionRepository) ListActive(ctx context.Context) ([]agentexecution.Execution, error) {
	values, err := list[agentexecution.Execution](ctx, r.storage, "agent_execution", func(value agentexecution.Execution) bool { return value.Active() })
	return sorted(values, executionOrder), err
}

func (r AgentExecutionRepository) ListBySession(ctx context.Context, id identity.SessionID) ([]agentexecution.Execution, error) {
	values, err := list[agentexecution.Execution](ctx, r.storage, "agent_execution", func(value agentexecution.Execution) bool { return value.SessionID == id })
	return sorted(values, executionOrder), err
}

func (r AgentExecutionRepository) ListByAgent(ctx context.Context, id identity.AgentID, limit int) ([]agentexecution.Execution, error) {
	values, err := list[agentexecution.Execution](ctx, r.storage, "agent_execution", func(value agentexecution.Execution) bool { return value.AgentID == id })
	return capped(sorted(values, func(a, b agentexecution.Execution) int { return -executionOrder(a, b) }), limit), err
}

func executionOrder(a, b agentexecution.Execution) int {
	return cmp.Or(a.CreatedAt.Compare(b.CreatedAt), cmp.Compare(a.ID, b.ID))
}
