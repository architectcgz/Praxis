package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domainagent "praxis/internal/domain/agent"
	domainfoundation "praxis/internal/domain/foundation"
)

func (s *Store) GetAgent(ctx context.Context, id domainfoundation.AgentID) (domainagent.Agent, error) {
	row := s.Executor(ctx).QueryRowContext(ctx, `SELECT id, session_id, profile, security_policy_revision, state, current_execution_id, created_at, updated_at FROM agents WHERE id = ?`, id.String())
	var value domainagent.Agent
	var createdAt, updatedAt string
	if err := row.Scan(&value.ID, &value.SessionID, &value.Profile, &value.SecurityPolicyRevision, &value.State, &value.CurrentExecutionID, &createdAt, &updatedAt); err == sql.ErrNoRows {
		return value, domainfoundation.ErrNotFound
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

func (s *Store) listAgents(ctx context.Context, query string, args ...any) ([]domainagent.Agent, error) {
	rows, err := s.Executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list agents: %w", err)
	}
	defer rows.Close()
	values := make([]domainagent.Agent, 0)
	for rows.Next() {
		var value domainagent.Agent
		var createdAt, updatedAt string
		if err := rows.Scan(&value.ID, &value.SessionID, &value.Profile, &value.SecurityPolicyRevision, &value.State, &value.CurrentExecutionID, &createdAt, &updatedAt); err != nil {
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
func (s *Store) SaveAgent(ctx context.Context, value domainagent.Agent) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.execMutation(ctx, `INSERT INTO agents (id, session_id, profile, security_policy_revision, state, current_execution_id, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id, profile = excluded.profile, security_policy_revision = excluded.security_policy_revision, state = excluded.state, current_execution_id = excluded.current_execution_id, created_at = excluded.created_at, updated_at = excluded.updated_at`, value.ID.String(), value.SessionID.String(), string(value.Profile), value.SecurityPolicyRevision, string(value.State), value.CurrentExecutionID.String(), value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano))
}
func (s *Store) ListAgentsBySession(ctx context.Context, sessionID domainfoundation.SessionID, limit int) ([]domainagent.Agent, error) {
	return s.listAgents(ctx, `SELECT id, session_id, profile, security_policy_revision, state, current_execution_id, created_at, updated_at FROM agents WHERE session_id = ? ORDER BY id LIMIT ?`, sessionID.String(), targetLimit(limit))
}
func (s *Store) ListAgentsAfter(ctx context.Context, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID, limit int) ([]domainagent.Agent, error) {
	return s.listAgents(ctx, `SELECT id, session_id, profile, security_policy_revision, state, current_execution_id, created_at, updated_at FROM agents WHERE (session_id > ? OR (session_id = ? AND id > ?)) ORDER BY session_id, id LIMIT ?`, sessionID.String(), sessionID.String(), agentID.String(), targetLimit(limit))
}

type AgentRepository struct{ store *Store }

func (r AgentRepository) Get(ctx context.Context, id domainfoundation.AgentID) (domainagent.Agent, error) {
	return r.store.GetAgent(ctx, id)
}
func (r AgentRepository) Save(ctx context.Context, value domainagent.Agent) error {
	return r.store.SaveAgent(ctx, value)
}
func (r AgentRepository) ListBySession(ctx context.Context, id domainfoundation.SessionID, limit int) ([]domainagent.Agent, error) {
	return r.store.ListAgentsBySession(ctx, id, limit)
}
func (r AgentRepository) ListAllAfter(ctx context.Context, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID, limit int) ([]domainagent.Agent, error) {
	return r.store.ListAgentsAfter(ctx, sessionID, agentID, limit)
}
