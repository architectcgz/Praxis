package sqlite

import (
	"context"
	"fmt"

	"praxis/internal/core/domain"
)

func (s *Store) GetQueuedWork(ctx context.Context, id domain.WorkItemID) (domain.QueuedWork, error) {
	return loadTargetPayload[domain.QueuedWork](
		ctx,
		s,
		`SELECT payload FROM queued_work_items WHERE id = ?`,
		[]any{id.String()},
		"queued work",
		func(value domain.QueuedWork) error { return value.Validate() },
	)
}

func (s *Store) SaveQueuedWork(ctx context.Context, value domain.QueuedWork) error {
	if err := value.Validate(); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO queued_work_items (
			id, session_id, agent_id, sequence, status, execution_id, created_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_id = excluded.session_id, agent_id = excluded.agent_id, sequence = excluded.sequence,
			status = excluded.status, execution_id = excluded.execution_id,
			created_at = excluded.created_at, payload = excluded.payload`,
		value.ID.String(),
		value.SessionID.String(),
		value.AgentID.String(),
		value.Sequence,
		string(value.Status),
		nullableID(value.ExecutionID),
		nullableTimeValue(value.CreatedAt),
		payload,
	)
	if err != nil && isConstraintError(err, "queued_work_items.agent_id, queued_work_items.sequence") {
		return fmt.Errorf("%w: queued work sequence for agent %s", domain.ErrRequestConflict, value.AgentID)
	}
	if err != nil && isConstraintError(err, "queued_work_items.execution_id") {
		return fmt.Errorf("%w: queued work execution %s", domain.ErrRequestConflict, value.ExecutionID)
	}
	return err
}

func (s *Store) NextQueuedWorkSequence(ctx context.Context, agentID domain.AgentID) (uint64, error) {
	var sequence uint64
	err := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT COALESCE(MAX(sequence), 0) + 1 FROM queued_work_items WHERE agent_id = ?`,
		agentID.String(),
	).Scan(&sequence)
	if err != nil {
		return 0, fmt.Errorf("allocate queued work sequence: %w", err)
	}
	return sequence, nil
}

func (s *Store) FindNextPendingQueuedWork(
	ctx context.Context,
	agentID domain.AgentID,
) (domain.QueuedWork, error) {
	return loadTargetPayload[domain.QueuedWork](
		ctx,
		s,
		`SELECT payload FROM queued_work_items
		 WHERE agent_id = ? AND status = 'queued' ORDER BY sequence, id LIMIT 1`,
		[]any{agentID.String()},
		"next queued work",
		func(value domain.QueuedWork) error { return value.Validate() },
	)
}

func (s *Store) ListRunningQueuedWork(ctx context.Context, limit int) ([]domain.QueuedWork, error) {
	return listTargetPayloads[domain.QueuedWork](
		ctx,
		s,
		`SELECT payload FROM queued_work_items WHERE status = 'running' ORDER BY created_at, id LIMIT ?`,
		[]any{targetLimit(limit)},
		"running queued work",
		func(value domain.QueuedWork) error { return value.Validate() },
	)
}

type QueuedWorkRepository struct{ store *Store }

func (r QueuedWorkRepository) Get(ctx context.Context, id domain.WorkItemID) (domain.QueuedWork, error) {
	return r.store.GetQueuedWork(ctx, id)
}

func (r QueuedWorkRepository) Save(ctx context.Context, value domain.QueuedWork) error {
	return r.store.SaveQueuedWork(ctx, value)
}

func (r QueuedWorkRepository) NextSequence(ctx context.Context, agentID domain.AgentID) (uint64, error) {
	return r.store.NextQueuedWorkSequence(ctx, agentID)
}

func (r QueuedWorkRepository) FindNextPendingByAgent(
	ctx context.Context,
	agentID domain.AgentID,
) (domain.QueuedWork, error) {
	return r.store.FindNextPendingQueuedWork(ctx, agentID)
}

func (r QueuedWorkRepository) ListRunning(ctx context.Context, limit int) ([]domain.QueuedWork, error) {
	return r.store.ListRunningQueuedWork(ctx, limit)
}
