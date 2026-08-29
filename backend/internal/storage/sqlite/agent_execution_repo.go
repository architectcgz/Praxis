package sqlite

import (
	"context"
	"fmt"

	"praxis/internal/core/domain"
)

func (s *Store) GetAgentExecution(
	ctx context.Context,
	id domain.AgentExecutionID,
) (domain.AgentExecution, error) {
	var value domain.AgentExecution
	return value, s.loadPayload(ctx, "agent_executions", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentExecution(ctx context.Context, value domain.AgentExecution) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO agent_executions (
			id, session_id, agent_id, request_id, work_item_id, reason, status, outcome,
			created_at, started_at, settled_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_id = excluded.session_id, agent_id = excluded.agent_id, request_id = excluded.request_id,
			work_item_id = excluded.work_item_id, reason = excluded.reason, status = excluded.status,
			outcome = excluded.outcome, created_at = excluded.created_at, started_at = excluded.started_at,
			settled_at = excluded.settled_at, payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), value.AgentID.String(), value.RequestID.String(),
		value.WorkItemID.String(), string(value.Reason), string(value.Status), string(value.Outcome),
		nullableTimeValue(value.CreatedAt),
		nullableTimeValue(value.StartedAt),
		nullableTimeValue(value.SettledAt),
		payload,
	)
	if err != nil && isConstraintError(err, "agent_executions.agent_id, agent_executions.request_id") {
		return fmt.Errorf("%w: %s", domain.ErrRequestConflict, value.RequestID)
	}
	if err != nil && isConstraintError(err, "agent_executions.agent_id") {
		return fmt.Errorf("%w: %s", domain.ErrAgentExecuting, value.AgentID)
	}
	return err
}

func (s *Store) FindExecutionByRequest(
	ctx context.Context,
	agentID domain.AgentID,
	requestID domain.RequestID,
) (domain.AgentExecution, error) {
	return loadTargetPayload[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions WHERE agent_id = ? AND request_id = ?`,
		[]any{agentID.String(), requestID.String()},
		"agent execution request",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) GetActiveExecutionByAgent(
	ctx context.Context,
	agentID domain.AgentID,
) (domain.AgentExecution, error) {
	return loadTargetPayload[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions
		 WHERE agent_id = ? AND status IN ('starting', 'running', 'settling')`,
		[]any{agentID.String()},
		"active agent execution",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) ListAgentExecutionsByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.AgentExecution, error) {
	return listTargetPayloads[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions WHERE agent_id = ?
		 ORDER BY created_at DESC, id DESC LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"agent executions",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) CountActiveExecutionsByGroup(ctx context.Context, groupID domain.AgentGroupID) (int, error) {
	var count int
	err := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM agent_executions execution
		 JOIN agents agent ON agent.id = execution.agent_id
		 WHERE agent.group_id = ? AND execution.status IN ('starting', 'running', 'settling')`,
		groupID.String(),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active executions for group: %w", err)
	}
	return count, nil
}

func (s *Store) ListRecoverableExecutions(ctx context.Context, limit int) ([]domain.AgentExecution, error) {
	return listTargetPayloads[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions
		 WHERE status IN ('starting', 'running', 'settling') ORDER BY created_at, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"recoverable agent executions",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) ListStartingExecutions(ctx context.Context, limit int) ([]domain.AgentExecution, error) {
	return listTargetPayloads[domain.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions WHERE status = 'starting' ORDER BY created_at, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"starting agent executions",
		func(value domain.AgentExecution) error { return value.Validate() },
	)
}

type AgentExecutionRepository struct{ store *Store }

func (r AgentExecutionRepository) Get(
	ctx context.Context,
	id domain.AgentExecutionID,
) (domain.AgentExecution, error) {
	return r.store.GetAgentExecution(ctx, id)
}

func (r AgentExecutionRepository) Save(ctx context.Context, value domain.AgentExecution) error {
	return r.store.SaveAgentExecution(ctx, value)
}

func (r AgentExecutionRepository) FindByRequest(
	ctx context.Context,
	agentID domain.AgentID,
	requestID domain.RequestID,
) (domain.AgentExecution, error) {
	return r.store.FindExecutionByRequest(ctx, agentID, requestID)
}

func (r AgentExecutionRepository) GetActiveByAgent(
	ctx context.Context,
	agentID domain.AgentID,
) (domain.AgentExecution, error) {
	return r.store.GetActiveExecutionByAgent(ctx, agentID)
}

func (r AgentExecutionRepository) ListByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.AgentExecution, error) {
	return r.store.ListAgentExecutionsByAgent(ctx, agentID, limit)
}

func (r AgentExecutionRepository) CountActiveByGroup(ctx context.Context, groupID domain.AgentGroupID) (int, error) {
	return r.store.CountActiveExecutionsByGroup(ctx, groupID)
}

func (r AgentExecutionRepository) ListRecoverable(
	ctx context.Context,
	limit int,
) ([]domain.AgentExecution, error) {
	return r.store.ListRecoverableExecutions(ctx, limit)
}

func (r AgentExecutionRepository) ListStarting(
	ctx context.Context,
	limit int,
) ([]domain.AgentExecution, error) {
	return r.store.ListStartingExecutions(ctx, limit)
}
