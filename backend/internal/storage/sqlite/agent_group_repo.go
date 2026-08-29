package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetAgentGroup(ctx context.Context, id domain.AgentGroupID) (domain.AgentGroup, error) {
	var value domain.AgentGroup
	return value, s.loadPayload(ctx, "agent_groups", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveAgentGroup(ctx context.Context, value domain.AgentGroup) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_groups (id, session_id, primary_agent_id, max_concurrent, payload)
		 VALUES (?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id,
		 primary_agent_id = excluded.primary_agent_id, max_concurrent = excluded.max_concurrent,
		 payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), value.PrimaryAgentID.String(), value.MaxConcurrent, payload,
	)
}

func (s *Store) ListAgentGroupsBySession(
	ctx context.Context,
	sessionID domain.SessionID,
	limit int,
) ([]domain.AgentGroup, error) {
	return listTargetPayloads[domain.AgentGroup](
		ctx,
		s,
		`SELECT payload FROM agent_groups WHERE session_id = ? ORDER BY id LIMIT ?`,
		[]any{sessionID.String(), targetLimit(limit)},
		"agent groups",
		func(value domain.AgentGroup) error { return value.Validate() },
	)
}

type AgentGroupRepository struct{ store *Store }

func (r AgentGroupRepository) Get(ctx context.Context, id domain.AgentGroupID) (domain.AgentGroup, error) {
	return r.store.GetAgentGroup(ctx, id)
}

func (r AgentGroupRepository) Save(ctx context.Context, value domain.AgentGroup) error {
	return r.store.SaveAgentGroup(ctx, value)
}

func (r AgentGroupRepository) ListBySession(
	ctx context.Context,
	sessionID domain.SessionID,
	limit int,
) ([]domain.AgentGroup, error) {
	return r.store.ListAgentGroupsBySession(ctx, sessionID, limit)
}
