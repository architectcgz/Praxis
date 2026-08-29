package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetAgentControlRequest(
	ctx context.Context,
	id domain.AgentControlRequestID,
) (domain.AgentControlRequest, error) {
	var value domain.AgentControlRequest
	return value, s.loadPayload(
		ctx,
		"agent_control_requests",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) SaveAgentControlRequest(ctx context.Context, value domain.AgentControlRequest) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO agent_control_requests (id, agent_id, target_execution_id, kind, status, payload)
		 VALUES (?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET agent_id = excluded.agent_id,
		 target_execution_id = excluded.target_execution_id, kind = excluded.kind,
		 status = excluded.status, payload = excluded.payload`,
		value.ID.String(), value.AgentID.String(), nullableID(value.TargetExecutionID), string(value.Kind),
		string(value.Status), payload,
	)
}

func (s *Store) ListOpenAgentControlRequestsByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.AgentControlRequest, error) {
	return listTargetPayloads[domain.AgentControlRequest](
		ctx,
		s,
		`SELECT payload FROM agent_control_requests
		 WHERE agent_id = ? AND status = 'requested' ORDER BY id LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"open agent control requests",
		func(value domain.AgentControlRequest) error { return value.Validate() },
	)
}

type AgentControlRequestRepository struct{ store *Store }

func (r AgentControlRequestRepository) Get(
	ctx context.Context,
	id domain.AgentControlRequestID,
) (domain.AgentControlRequest, error) {
	return r.store.GetAgentControlRequest(ctx, id)
}

func (r AgentControlRequestRepository) Save(ctx context.Context, value domain.AgentControlRequest) error {
	return r.store.SaveAgentControlRequest(ctx, value)
}

func (r AgentControlRequestRepository) ListOpenByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.AgentControlRequest, error) {
	return r.store.ListOpenAgentControlRequestsByAgent(ctx, agentID, limit)
}
