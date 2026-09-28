package sqlite

import (
	agentmodel "praxis/internal/agent"
	"praxis/internal/contracts"

	"context"
	"database/sql"
	"fmt"
	"time"
)

func (s *Store) GetAgent(ctx context.Context, id contracts.AgentID) (agentmodel.Agent, error) {
	row := s.Executor(ctx).QueryRowContext(ctx, `SELECT id, session_id, definition_id, profile, security_policy_revision, state, current_execution_id, created_at, updated_at FROM agents WHERE id = ?`, id.String())
	var value agentmodel.Agent
	var createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.SessionID, &value.DefinitionID, &value.Profile, &value.SecurityPolicyRevision, &value.State, &value.CurrentExecutionID, &createdAt, &updatedAt); err == sql.ErrNoRows {
		return value, contracts.ErrNotFound
	} else if err != nil {
		return value, fmt.Errorf("read agent: %w", err)
	}
	var err error
	value.CreatedAt, err = parseTimestamp(createdAt, "agent.createdAt")
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = parseTimestamp(updatedAt, "agent.updatedAt")
	if err != nil {
		return value, err
	}
	return value, value.Validate()
}

func (s *Store) listAgents(ctx context.Context, query string, args ...any) ([]agentmodel.Agent, error) {
	rows, err := s.Executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	values := make([]agentmodel.Agent, 0)
	for rows.Next() {
		var value agentmodel.Agent
		var createdAt, updatedAt string
		if err := rows.Scan(&value.ID, &value.SessionID, &value.DefinitionID, &value.Profile, &value.SecurityPolicyRevision, &value.State, &value.CurrentExecutionID, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		value.CreatedAt, err = parseTimestamp(createdAt, "agent.createdAt")
		if err != nil {
			return nil, err
		}
		value.UpdatedAt, err = parseTimestamp(updatedAt, "agent.updatedAt")
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
func (s *Store) SaveAgent(ctx context.Context, value agentmodel.Agent) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.execMutation(ctx, `INSERT INTO agents (id, session_id, definition_id, profile, security_policy_revision, state, current_execution_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id, definition_id = excluded.definition_id, profile = excluded.profile, security_policy_revision = excluded.security_policy_revision, state = excluded.state, current_execution_id = excluded.current_execution_id, created_at = excluded.created_at, updated_at = excluded.updated_at`, value.ID.String(), value.SessionID.String(), value.DefinitionID.String(), string(value.Profile), value.SecurityPolicyRevision, string(value.State), value.CurrentExecutionID.String(), value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano))
}
func (s *Store) ListAgentsBySession(ctx context.Context, sessionID contracts.SessionID, limit int) ([]agentmodel.Agent, error) {
	return s.listAgents(ctx, `SELECT id, session_id, definition_id, profile, security_policy_revision, state, current_execution_id, created_at, updated_at FROM agents WHERE session_id = ? ORDER BY id LIMIT ?`, sessionID.String(), targetLimit(limit))
}

func (s *Store) GetAgentBySessionAndDefinition(ctx context.Context, sessionID contracts.SessionID, definitionID contracts.AgentDefinitionID) (agentmodel.Agent, error) {
	values, err := s.listAgents(ctx, `SELECT id, session_id, definition_id, profile, security_policy_revision, state, current_execution_id, created_at, updated_at FROM agents WHERE session_id = ? AND definition_id = ? ORDER BY id LIMIT 1`, sessionID.String(), definitionID.String())
	if err != nil {
		return agentmodel.Agent{}, err
	}
	if len(values) == 0 {
		return agentmodel.Agent{}, contracts.ErrNotFound
	}
	return values[0], nil
}

type SessionAgentRepository struct{ store *Store }

func (r SessionAgentRepository) Get(ctx context.Context, id contracts.AgentID) (agentmodel.Agent, error) {
	return r.store.GetAgent(ctx, id)
}
func (r SessionAgentRepository) GetBySessionAndDefinition(ctx context.Context, sessionID contracts.SessionID, definitionID contracts.AgentDefinitionID) (agentmodel.Agent, error) {
	return r.store.GetAgentBySessionAndDefinition(ctx, sessionID, definitionID)
}
func (r SessionAgentRepository) Save(ctx context.Context, value agentmodel.Agent) error {
	return r.store.SaveAgent(ctx, value)
}
func (r SessionAgentRepository) ListBySession(ctx context.Context, id contracts.SessionID, limit int) ([]agentmodel.Agent, error) {
	return r.store.ListAgentsBySession(ctx, id, limit)
}
