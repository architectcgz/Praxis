package sqlite

import (
	"context"
	domainworkflow "praxis/internal/domain/workflow"

	domainfoundation "praxis/internal/domain/foundation"
)

func (s *Store) GetAgentControlRequest(
	ctx context.Context,
	id domainfoundation.AgentControlRequestID,
) (domainworkflow.AgentControlRequest, error) {
	var value domainworkflow.AgentControlRequest
	return value, s.loadPayload(
		ctx,
		"agent_control_requests",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) SaveAgentControlRequest(ctx context.Context, value domainworkflow.AgentControlRequest) error {
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
	agentID domainfoundation.AgentID,
	limit int,
) ([]domainworkflow.AgentControlRequest, error) {
	return listTargetPayloads[domainworkflow.AgentControlRequest](
		ctx,
		s,
		`SELECT payload FROM agent_control_requests
		 WHERE agent_id = ? AND status = 'requested' ORDER BY id LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"open agent control requests",
		func(value domainworkflow.AgentControlRequest) error { return value.Validate() },
	)
}

func (s *Store) ListOpenAgentControlRequestsByAgentAfter(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	afterID domainfoundation.AgentControlRequestID,
	limit int,
) ([]domainworkflow.AgentControlRequest, error) {
	return listTargetPayloads[domainworkflow.AgentControlRequest](ctx, s, `
		SELECT payload FROM agent_control_requests
		 WHERE agent_id = ? AND status = 'requested' AND id > ? ORDER BY id LIMIT ?`,
		[]any{agentID.String(), afterID.String(), targetLimit(limit)}, "open agent control requests", func(value domainworkflow.AgentControlRequest) error {
			return value.Validate()
		})
}

type AgentControlRequestRepository struct{ store *Store }

func (r AgentControlRequestRepository) Get(
	ctx context.Context,
	id domainfoundation.AgentControlRequestID,
) (domainworkflow.AgentControlRequest, error) {
	return r.store.GetAgentControlRequest(ctx, id)
}

func (r AgentControlRequestRepository) Save(ctx context.Context, value domainworkflow.AgentControlRequest) error {
	return r.store.SaveAgentControlRequest(ctx, value)
}

func (r AgentControlRequestRepository) ListOpenByAgent(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	limit int,
) ([]domainworkflow.AgentControlRequest, error) {
	return r.store.ListOpenAgentControlRequestsByAgent(ctx, agentID, limit)
}

func (r AgentControlRequestRepository) ListOpenByAgentAfter(ctx context.Context, agentID domainfoundation.AgentID, afterID domainfoundation.AgentControlRequestID, limit int) ([]domainworkflow.AgentControlRequest, error) {
	return r.store.ListOpenAgentControlRequestsByAgentAfter(ctx, agentID, afterID, limit)
}
