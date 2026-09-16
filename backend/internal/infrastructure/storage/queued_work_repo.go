package storage

import (
	"context"
	"fmt"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
)

func (s *Store) GetQueuedWork(ctx context.Context, id domainfoundation.WorkItemID) (domainworkflow.QueuedWork, error) {
	return loadDocumentRef[domainworkflow.QueuedWork](ctx, s, `SELECT document_ref FROM queued_work_items WHERE id = ?`, []any{id.String()}, "queued work", func(value domainworkflow.QueuedWork) error { return value.Validate() })
}
func (s *Store) SaveQueuedWork(ctx context.Context, value domainworkflow.QueuedWork) error {
	if err := value.Validate(); err != nil {
		return err
	}
	ref, err := s.putDocument(ctx, "queued-work", value.ID.String(), value)
	if err != nil {
		return err
	}
	err = s.saveMetadata(ctx, `INSERT INTO queued_work_items (id, session_id, agent_id, sequence, status, execution_id, created_at, started_at, finished_at, failure_code, document_ref) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id, agent_id = excluded.agent_id, sequence = excluded.sequence, status = excluded.status, execution_id = excluded.execution_id, created_at = excluded.created_at, started_at = excluded.started_at, finished_at = excluded.finished_at, failure_code = excluded.failure_code, document_ref = excluded.document_ref`, value.ID.String(), value.SessionID.String(), value.AgentID.String(), value.Sequence, string(value.Status), nullableID(value.ExecutionID), value.CreatedAt.UTC().Format("2006-01-02T15:04:05.999999999Z07:00"), nullableTimeValue(value.StartedAt), nullableTimeValue(value.FinishedAt), string(value.FailureCode), ref)
	if err != nil && isConstraintError(err, "queued_work_items.agent_id, queued_work_items.sequence") {
		return fmt.Errorf("%w: queued work sequence for agent %s", domainfoundation.ErrRequestConflict, value.AgentID)
	}
	if err != nil && isConstraintError(err, "queued_work_items.execution_id") {
		return fmt.Errorf("%w: queued work execution %s", domainfoundation.ErrRequestConflict, value.ExecutionID)
	}
	return err
}
func (s *Store) NextQueuedWorkSequence(ctx context.Context, agentID domainfoundation.AgentID) (uint64, error) {
	var sequence uint64
	if err := s.executor(ctx).QueryRowContext(ctx, `SELECT COALESCE(MAX(sequence), 0) + 1 FROM queued_work_items WHERE agent_id = ?`, agentID.String()).Scan(&sequence); err != nil {
		return 0, fmt.Errorf("allocate queued work sequence: %w", err)
	}
	return sequence, nil
}
func (s *Store) FindNextPendingQueuedWork(ctx context.Context, agentID domainfoundation.AgentID) (domainworkflow.QueuedWork, error) {
	return loadDocumentRef[domainworkflow.QueuedWork](ctx, s, `SELECT document_ref FROM queued_work_items WHERE agent_id = ? AND status = 'queued' ORDER BY sequence, id LIMIT 1`, []any{agentID.String()}, "next queued work", func(value domainworkflow.QueuedWork) error { return value.Validate() })
}
func (s *Store) ListRunningQueuedWork(ctx context.Context, limit int) ([]domainworkflow.QueuedWork, error) {
	return listDocumentRefs[domainworkflow.QueuedWork](ctx, s, `SELECT document_ref FROM queued_work_items WHERE status = 'running' ORDER BY created_at, id LIMIT ?`, []any{targetLimit(limit)}, "running queued work", func(value domainworkflow.QueuedWork) error { return value.Validate() })
}

type QueuedWorkRepository struct{ store *Store }

func (r QueuedWorkRepository) Get(ctx context.Context, id domainfoundation.WorkItemID) (domainworkflow.QueuedWork, error) {
	return r.store.GetQueuedWork(ctx, id)
}
func (r QueuedWorkRepository) Save(ctx context.Context, value domainworkflow.QueuedWork) error {
	return r.store.SaveQueuedWork(ctx, value)
}
func (r QueuedWorkRepository) NextSequence(ctx context.Context, id domainfoundation.AgentID) (uint64, error) {
	return r.store.NextQueuedWorkSequence(ctx, id)
}
func (r QueuedWorkRepository) FindNextPendingByAgent(ctx context.Context, id domainfoundation.AgentID) (domainworkflow.QueuedWork, error) {
	return r.store.FindNextPendingQueuedWork(ctx, id)
}
func (r QueuedWorkRepository) ListRunning(ctx context.Context, limit int) ([]domainworkflow.QueuedWork, error) {
	return r.store.ListRunningQueuedWork(ctx, limit)
}
