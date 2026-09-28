package sqlite

import (
	"praxis/internal/contracts"
	workflowmodel "praxis/internal/workflow"

	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) GetAgentControlCommand(ctx context.Context, id contracts.AgentControlCommandID) (workflowmodel.AgentControlCommand, error) {
	row := s.Executor(ctx).QueryRowContext(ctx, `SELECT id, agent_id, target_execution_id, kind, status, created_at, applied_at FROM agent_control_commands WHERE id = ?`, id.String())
	var value workflowmodel.AgentControlCommand
	var target, createdAt, appliedAt sql.NullString
	if err := row.Scan(&value.ID, &value.AgentID, &target, &value.Kind, &value.Status, &createdAt, &appliedAt); err == sql.ErrNoRows {
		return value, contracts.ErrNotFound
	} else if err != nil {
		return value, fmt.Errorf("read control request: %w", err)
	}
	value.TargetExecutionID = contracts.AgentExecutionID(target.String)
	var err error
	value.CreatedAt, err = parseTimestamp(createdAt.String, "control.createdAt")
	if err != nil {
		return value, err
	}
	value.AppliedAt, err = parseNullableTimestamp(appliedAt, "control.appliedAt")
	if err != nil {
		return value, err
	}
	return value, value.Validate()
}

func (s *Store) SaveAgentControlCommand(ctx context.Context, value workflowmodel.AgentControlCommand) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.execMutation(ctx, `INSERT INTO agent_control_commands (id, agent_id, target_execution_id, kind, status, created_at, applied_at) VALUES (?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET agent_id = excluded.agent_id, target_execution_id = excluded.target_execution_id, kind = excluded.kind, status = excluded.status, created_at = excluded.created_at, applied_at = excluded.applied_at`, value.ID.String(), value.AgentID.String(), nullableID(value.TargetExecutionID), string(value.Kind), string(value.Status), value.CreatedAt.UTC().Format(time.RFC3339Nano), nullableTimeValue(value.AppliedAt))
}

func (s *Store) listControlCommands(ctx context.Context, query string, args ...any) ([]workflowmodel.AgentControlCommand, error) {
	rows, err := s.Executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list control requests: %w", err)
	}
	defer rows.Close()
	values := make([]workflowmodel.AgentControlCommand, 0)
	for rows.Next() {
		var value workflowmodel.AgentControlCommand
		var target, createdAt, appliedAt sql.NullString
		if err := rows.Scan(&value.ID, &value.AgentID, &target, &value.Kind, &value.Status, &createdAt, &appliedAt); err != nil {
			return nil, err
		}
		value.TargetExecutionID = contracts.AgentExecutionID(target.String)
		value.CreatedAt, err = parseTimestamp(createdAt.String, "control.createdAt")
		if err != nil {
			return nil, err
		}
		value.AppliedAt, err = parseNullableTimestamp(appliedAt, "control.appliedAt")
		if err != nil {
			return nil, err
		}
		if err := value.Validate(); err != nil {
			return nil, err
		}
		values = append(values, value)
	}
	return values, rows.Err()
}
func (s *Store) ListPendingAgentControlCommandsByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]workflowmodel.AgentControlCommand, error) {
	return s.listControlCommands(ctx, `SELECT id, agent_id, target_execution_id, kind, status, created_at, applied_at FROM agent_control_commands WHERE agent_id = ? AND status = 'pending' ORDER BY id LIMIT ?`, agentID.String(), targetLimit(limit))
}

type AgentControlCommandRepository struct{ store *Store }

func (r AgentControlCommandRepository) Get(ctx context.Context, id contracts.AgentControlCommandID) (workflowmodel.AgentControlCommand, error) {
	return r.store.GetAgentControlCommand(ctx, id)
}
func (r AgentControlCommandRepository) Save(ctx context.Context, value workflowmodel.AgentControlCommand) error {
	return r.store.SaveAgentControlCommand(ctx, value)
}
func (r AgentControlCommandRepository) ListOpenByAgent(ctx context.Context, id contracts.AgentID, limit int) ([]workflowmodel.AgentControlCommand, error) {
	return r.store.ListPendingAgentControlCommandsByAgent(ctx, id, limit)
}
