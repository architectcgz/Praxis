package sqlite

import (
	"context"
	"time"

	"praxis/internal/core/domain"
)

func (s *Store) GetAgent(ctx context.Context, id domain.AgentID) (domain.Agent, error) {
	var value domain.Agent
	return value, s.loadPayload(ctx, "agents", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgent(ctx context.Context, value domain.Agent) error {
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
			id, session_id, group_id, profile, task_packet_id, context_manifest_id,
			capability_grant_id, state, current_execution_id, created_at, updated_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
			session_id = excluded.session_id, group_id = excluded.group_id, profile = excluded.profile,
			task_packet_id = excluded.task_packet_id, context_manifest_id = excluded.context_manifest_id,
			capability_grant_id = excluded.capability_grant_id, state = excluded.state,
			current_execution_id = excluded.current_execution_id, created_at = excluded.created_at,
			updated_at = excluded.updated_at, payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), value.GroupID.String(), string(value.Profile),
		value.TaskPacketID.String(), value.ContextManifestID.String(), value.GrantID.String(), string(value.State),
		value.CurrentExecutionID.String(), value.CreatedAt.UTC().Format(time.RFC3339Nano),
		value.UpdatedAt.UTC().Format(time.RFC3339Nano), payload,
	)
}

func (s *Store) ListAgentsByGroup(
	ctx context.Context,
	groupID domain.AgentGroupID,
	limit int,
) ([]domain.Agent, error) {
	return listTargetPayloads[domain.Agent](
		ctx,
		s,
		`SELECT payload FROM agents WHERE group_id = ? ORDER BY id LIMIT ?`,
		[]any{groupID.String(), targetLimit(limit)},
		"agents",
		func(value domain.Agent) error { return value.Validate() },
	)
}

func (s *Store) ListAgents(ctx context.Context, limit int) ([]domain.Agent, error) {
	return listTargetPayloads[domain.Agent](
		ctx,
		s,
		`SELECT payload FROM agents ORDER BY session_id, group_id, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"agents",
		func(value domain.Agent) error { return value.Validate() },
	)
}

type AgentRepository struct{ store *Store }

func (r AgentRepository) Get(ctx context.Context, id domain.AgentID) (domain.Agent, error) {
	return r.store.GetAgent(ctx, id)
}

func (r AgentRepository) Save(ctx context.Context, value domain.Agent) error {
	return r.store.SaveAgent(ctx, value)
}

func (r AgentRepository) ListByGroup(
	ctx context.Context,
	groupID domain.AgentGroupID,
	limit int,
) ([]domain.Agent, error) {
	return r.store.ListAgentsByGroup(ctx, groupID, limit)
}

func (r AgentRepository) ListAll(ctx context.Context, limit int) ([]domain.Agent, error) {
	return r.store.ListAgents(ctx, limit)
}
