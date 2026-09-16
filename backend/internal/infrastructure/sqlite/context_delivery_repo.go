package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	domainfoundation "praxis/internal/domain/foundation"
	domainworkflow "praxis/internal/domain/workflow"
	"praxis/internal/persistence"
)

func scanContextDelivery(row *sql.Row) (domainworkflow.ContextDelivery, error) {
	var value domainworkflow.ContextDelivery
	var createdAt, updatedAt string
	var resultExecutionID sql.NullString
	if err := row.Scan(&value.ID, &value.SessionID, &value.SourceArtifactID, &value.TargetAgentID, &value.DedupeKey, &value.Status, &value.ArtifactEntryRef, &resultExecutionID, &value.FailureCode, &createdAt, &updatedAt); err != nil {
		return value, err
	}
	value.ResultExecutionID = domainfoundation.AgentExecutionID(resultExecutionID.String)
	var err error
	value.CreatedAt, err = parseTimestamp(createdAt, "context delivery.createdAt")
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = parseTimestamp(updatedAt, "context delivery.updatedAt")
	if err != nil {
		return value, err
	}
	return value, value.Validate()
}
func (s *Store) GetContextDelivery(ctx context.Context, id domainfoundation.DeliveryID) (domainworkflow.ContextDelivery, error) {
	value, err := scanContextDelivery(s.Executor(ctx).QueryRowContext(ctx, `SELECT id, session_id, source_artifact_id, target_agent_id, dedupe_key, status, artifact_entry_ref, result_execution_id, failure_code, created_at, updated_at FROM context_deliveries WHERE id = ?`, id.String()))
	if err == sql.ErrNoRows {
		return value, domainfoundation.ErrNotFound
	}
	return value, err
}
func (s *Store) SaveContextDelivery(ctx context.Context, value domainworkflow.ContextDelivery) error {
	if err := value.Validate(); err != nil {
		return err
	}
	return s.execMutation(ctx, `INSERT INTO context_deliveries (id, session_id, source_artifact_id, target_agent_id, dedupe_key, status, artifact_entry_ref, result_execution_id, failure_code, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET session_id = excluded.session_id, source_artifact_id = excluded.source_artifact_id, target_agent_id = excluded.target_agent_id, dedupe_key = excluded.dedupe_key, status = excluded.status, artifact_entry_ref = excluded.artifact_entry_ref, result_execution_id = excluded.result_execution_id, failure_code = excluded.failure_code, created_at = excluded.created_at, updated_at = excluded.updated_at`, value.ID.String(), value.SessionID.String(), value.SourceArtifactID, value.TargetAgentID.String(), value.DedupeKey, string(value.Status), value.ArtifactEntryRef, nullableID(value.ResultExecutionID), value.FailureCode, value.CreatedAt.UTC().Format(time.RFC3339Nano), value.UpdatedAt.UTC().Format(time.RFC3339Nano))
}
func (s *Store) listContextDeliveries(ctx context.Context, query string, args ...any) ([]domainworkflow.ContextDelivery, error) {
	rows, err := s.Executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list context deliveries: %w", err)
	}
	defer rows.Close()
	values := make([]domainworkflow.ContextDelivery, 0)
	for rows.Next() {
		var value domainworkflow.ContextDelivery
		var createdAt, updatedAt string
		var resultExecutionID sql.NullString
		if err := rows.Scan(&value.ID, &value.SessionID, &value.SourceArtifactID, &value.TargetAgentID, &value.DedupeKey, &value.Status, &value.ArtifactEntryRef, &resultExecutionID, &value.FailureCode, &createdAt, &updatedAt); err != nil {
			return nil, err
		}
		value.ResultExecutionID = domainfoundation.AgentExecutionID(resultExecutionID.String)
		value.CreatedAt, err = parseTimestamp(createdAt, "context delivery.createdAt")
		if err != nil {
			return nil, err
		}
		value.UpdatedAt, err = parseTimestamp(updatedAt, "context delivery.updatedAt")
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
func (s *Store) ListPendingContextDeliveriesByTarget(ctx context.Context, id domainfoundation.AgentID, limit int) ([]domainworkflow.ContextDelivery, error) {
	return s.listContextDeliveries(ctx, `SELECT id, session_id, source_artifact_id, target_agent_id, dedupe_key, status, artifact_entry_ref, result_execution_id, failure_code, created_at, updated_at FROM context_deliveries WHERE target_agent_id = ? AND status IN ('pending', 'delivering') ORDER BY id LIMIT ?`, id.String(), targetLimit(limit))
}
func (s *Store) ListInFlightContextDeliveries(ctx context.Context, limit int) ([]domainworkflow.ContextDelivery, error) {
	return s.listContextDeliveries(ctx, `SELECT id, session_id, source_artifact_id, target_agent_id, dedupe_key, status, artifact_entry_ref, result_execution_id, failure_code, created_at, updated_at FROM context_deliveries WHERE status IN ('pending', 'delivering') ORDER BY id LIMIT ?`, targetLimit(limit))
}
func (s *Store) ListInFlightContextDeliveriesAfter(ctx context.Context, id domainfoundation.DeliveryID, limit int) ([]domainworkflow.ContextDelivery, error) {
	return s.listContextDeliveries(ctx, `SELECT id, session_id, source_artifact_id, target_agent_id, dedupe_key, status, artifact_entry_ref, result_execution_id, failure_code, created_at, updated_at FROM context_deliveries WHERE status IN ('pending', 'delivering') AND id > ? ORDER BY id LIMIT ?`, id.String(), targetLimit(limit))
}
func (s *Store) HasDeliveringContextDeliveryByTarget(ctx context.Context, id domainfoundation.AgentID) (bool, error) {
	var exists bool
	err := s.Executor(ctx).QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM context_deliveries WHERE target_agent_id = ? AND status = 'delivering')`, id.String()).Scan(&exists)
	return exists, err
}

type ContextDeliveryRepository struct{ store *Store }

func (r ContextDeliveryRepository) Get(ctx context.Context, id domainfoundation.DeliveryID) (domainworkflow.ContextDelivery, error) {
	return r.store.GetContextDelivery(ctx, id)
}
func (r ContextDeliveryRepository) Save(ctx context.Context, value domainworkflow.ContextDelivery) error {
	return r.store.SaveContextDelivery(ctx, value)
}
func (r ContextDeliveryRepository) ListPendingByTarget(ctx context.Context, id domainfoundation.AgentID, limit int) ([]domainworkflow.ContextDelivery, error) {
	return r.store.ListPendingContextDeliveriesByTarget(ctx, id, limit)
}
func (r ContextDeliveryRepository) ListInFlight(ctx context.Context, limit int) ([]domainworkflow.ContextDelivery, error) {
	return r.store.ListInFlightContextDeliveries(ctx, limit)
}
func (r ContextDeliveryRepository) ListInFlightAfter(ctx context.Context, id domainfoundation.DeliveryID, limit int) ([]domainworkflow.ContextDelivery, error) {
	return r.store.ListInFlightContextDeliveriesAfter(ctx, id, limit)
}
func (r ContextDeliveryRepository) HasDeliveringByTarget(ctx context.Context, id domainfoundation.AgentID) (bool, error) {
	return r.store.HasDeliveringContextDeliveryByTarget(ctx, id)
}

var _ persistence.ContextDeliveryRepository = ContextDeliveryRepository{}
