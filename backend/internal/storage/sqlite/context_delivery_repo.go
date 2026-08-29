package sqlite

import (
	"context"
	"fmt"

	"praxis/internal/core/domain"
	"praxis/internal/core/persistence"
)

func (s *Store) GetContextDelivery(ctx context.Context, id domain.DeliveryID) (domain.ContextDelivery, error) {
	var value domain.ContextDelivery
	return value, s.loadPayload(
		ctx,
		"context_deliveries",
		id.String(),
		&value,
		func() error { return value.Validate() },
	)
}

func (s *Store) SaveContextDelivery(ctx context.Context, value domain.ContextDelivery) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO context_deliveries (
			id, session_id, source_artifact_id, target_agent_id, dedupe_key, status, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id,
		source_artifact_id = excluded.source_artifact_id, target_agent_id = excluded.target_agent_id,
		dedupe_key = excluded.dedupe_key, status = excluded.status, payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), value.SourceArtifactID, value.TargetAgentID.String(),
		value.DedupeKey, string(value.Status), payload,
	)
}

func (s *Store) ListPendingContextDeliveriesByTarget(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.ContextDelivery, error) {
	return listTargetPayloads[domain.ContextDelivery](
		ctx,
		s,
		`SELECT payload FROM context_deliveries
		 WHERE target_agent_id = ? AND status IN ('pending', 'delivering') ORDER BY id LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"pending context deliveries",
		func(value domain.ContextDelivery) error { return value.Validate() },
	)
}

func (s *Store) ListInFlightContextDeliveries(ctx context.Context, limit int) ([]domain.ContextDelivery, error) {
	return listTargetPayloads[domain.ContextDelivery](
		ctx,
		s,
		`SELECT payload FROM context_deliveries
		 WHERE status IN ('pending', 'delivering') ORDER BY id LIMIT ?`,
		[]any{targetLimit(limit)},
		"in-flight context deliveries",
		func(value domain.ContextDelivery) error { return value.Validate() },
	)
}

func (s *Store) HasDeliveringContextDeliveryByTarget(ctx context.Context, agentID domain.AgentID) (bool, error) {
	var exists bool
	err := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM context_deliveries WHERE target_agent_id = ? AND status = 'delivering'
		)`,
		agentID.String(),
	).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("inspect delivering context delivery: %w", err)
	}
	return exists, nil
}

type ContextDeliveryRepository struct{ store *Store }

func (r ContextDeliveryRepository) Get(
	ctx context.Context,
	id domain.DeliveryID,
) (domain.ContextDelivery, error) {
	return r.store.GetContextDelivery(ctx, id)
}

func (r ContextDeliveryRepository) Save(ctx context.Context, value domain.ContextDelivery) error {
	return r.store.SaveContextDelivery(ctx, value)
}

func (r ContextDeliveryRepository) ListPendingByTarget(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.ContextDelivery, error) {
	return r.store.ListPendingContextDeliveriesByTarget(ctx, agentID, limit)
}

func (r ContextDeliveryRepository) ListInFlight(
	ctx context.Context,
	limit int,
) ([]domain.ContextDelivery, error) {
	return r.store.ListInFlightContextDeliveries(ctx, limit)
}

func (r ContextDeliveryRepository) HasDeliveringByTarget(ctx context.Context, agentID domain.AgentID) (bool, error) {
	return r.store.HasDeliveringContextDeliveryByTarget(ctx, agentID)
}

var (
	_ persistence.SessionRepository             = SessionRepository{}
	_ persistence.AgentGroupRepository          = AgentGroupRepository{}
	_ persistence.AgentRepository               = AgentRepository{}
	_ persistence.AgentExecutionRepository      = AgentExecutionRepository{}
	_ persistence.QueuedWorkRepository          = QueuedWorkRepository{}
	_ persistence.WaitConditionRepository       = WaitConditionRepository{}
	_ persistence.AgentControlRequestRepository = AgentControlRequestRepository{}
	_ persistence.ContextDeliveryRepository     = ContextDeliveryRepository{}
)
