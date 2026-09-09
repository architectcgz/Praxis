package sqlite

import (
	"context"
	"time"

	domainagent "praxis/internal/domain/agent"
	domainfoundation "praxis/internal/domain/foundation"
)

func (s *Store) GetAgent(ctx context.Context, id domainfoundation.AgentID) (domainagent.Agent, error) {
	var value domainagent.Agent
	return value, s.loadPayload(ctx, "agents", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgent(ctx context.Context, value domainagent.Agent) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agents (
			id, session_id, profile, security_policy_revision, state, current_execution_id,
			created_at, updated_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(id) DO UPDATE SET
			session_id = excluded.session_id, profile = excluded.profile,
			security_policy_revision = excluded.security_policy_revision, state = excluded.state,
			current_execution_id = excluded.current_execution_id, created_at = excluded.created_at,
			updated_at = excluded.updated_at, payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), string(value.Profile), value.SecurityPolicyRevision, string(value.State),
		value.CurrentExecutionID.String(), value.CreatedAt.UTC().Format(time.RFC3339Nano),
		value.UpdatedAt.UTC().Format(time.RFC3339Nano), payload,
	)
}

func (s *Store) ListAgentsBySession(
	ctx context.Context,
	sessionID domainfoundation.SessionID,
	limit int,
) ([]domainagent.Agent, error) {
	return listTargetPayloads[domainagent.Agent](
		ctx,
		s,
		`SELECT payload FROM agents WHERE session_id = ? ORDER BY id LIMIT ?`,
		[]any{sessionID.String(), targetLimit(limit)},
		"agents",
		func(value domainagent.Agent) error { return value.Validate() },
	)
}

func (s *Store) ListAgentsAfter(ctx context.Context, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID, limit int) ([]domainagent.Agent, error) {
	return listTargetPayloads[domainagent.Agent](ctx, s,
		`SELECT payload FROM agents WHERE (session_id > ? OR (session_id = ? AND id > ?)) ORDER BY session_id, id LIMIT ?`,
		[]any{sessionID.String(), sessionID.String(), agentID.String(), targetLimit(limit)}, "agents", func(value domainagent.Agent) error { return value.Validate() })
}

type AgentRepository struct{ store *Store }

func (r AgentRepository) Get(ctx context.Context, id domainfoundation.AgentID) (domainagent.Agent, error) {
	return r.store.GetAgent(ctx, id)
}

func (r AgentRepository) Save(ctx context.Context, value domainagent.Agent) error {
	return r.store.SaveAgent(ctx, value)
}

func (r AgentRepository) ListBySession(
	ctx context.Context,
	sessionID domainfoundation.SessionID,
	limit int,
) ([]domainagent.Agent, error) {
	return r.store.ListAgentsBySession(ctx, sessionID, limit)
}

func (r AgentRepository) ListAllAfter(ctx context.Context, sessionID domainfoundation.SessionID, agentID domainfoundation.AgentID, limit int) ([]domainagent.Agent, error) {
	return r.store.ListAgentsAfter(ctx, sessionID, agentID, limit)
}
