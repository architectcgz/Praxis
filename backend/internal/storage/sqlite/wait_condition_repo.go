package sqlite

import (
	"context"

	"praxis/internal/core/domain"
)

func (s *Store) GetWaitCondition(
	ctx context.Context,
	id domain.WaitConditionID,
) (domain.WaitCondition, error) {
	var value domain.WaitCondition
	return value, s.loadPayload(ctx, "wait_conditions", id.String(), &value, func() error { return value.Validate() })
}

func (s *Store) SaveWaitCondition(ctx context.Context, value domain.WaitCondition) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	return s.savePayload(
		ctx,
		`INSERT INTO wait_conditions (id, agent_id, execution_id, kind, mode, status, payload)
		 VALUES (?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET agent_id = excluded.agent_id,
		 execution_id = excluded.execution_id, kind = excluded.kind, mode = excluded.mode,
		 status = excluded.status,
		 payload = excluded.payload`,
		value.ID.String(),
		value.AgentID.String(),
		value.ExecutionID.String(),
		string(value.Kind),
		string(value.Mode),
		string(value.Status),
		payload,
	)
}

func (s *Store) ListUnresolvedWaitConditionsByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.WaitCondition, error) {
	return listTargetPayloads[domain.WaitCondition](
		ctx,
		s,
		`SELECT payload FROM wait_conditions WHERE agent_id = ? AND status = 'pending' ORDER BY id LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"unresolved wait conditions",
		func(value domain.WaitCondition) error { return value.Validate() },
	)
}

type WaitConditionRepository struct{ store *Store }

func (r WaitConditionRepository) Get(
	ctx context.Context,
	id domain.WaitConditionID,
) (domain.WaitCondition, error) {
	return r.store.GetWaitCondition(ctx, id)
}

func (r WaitConditionRepository) Save(ctx context.Context, value domain.WaitCondition) error {
	return r.store.SaveWaitCondition(ctx, value)
}

func (r WaitConditionRepository) ListUnresolvedByAgent(
	ctx context.Context,
	agentID domain.AgentID,
	limit int,
) ([]domain.WaitCondition, error) {
	return r.store.ListUnresolvedWaitConditionsByAgent(ctx, agentID, limit)
}
