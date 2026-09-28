package storage

import (
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"time"

	"praxis/internal/infra/sqlite"
)

func (s *Store) GetAgentExecution(ctx context.Context, id contracts.AgentExecutionID) (executionmodel.AgentExecution, error) {
	return s.loadExecutionByQuery(ctx,
		`SELECT context_revision, context_digest, document_ref FROM agent_executions WHERE id = ?`,
		[]any{id.String()}, "agent execution")
}

func (s *Store) validateStoredExecution(ctx context.Context, value executionmodel.AgentExecution) error {
	security, err := s.GetExecutionSecuritySnapshot(ctx, value.ID)
	if err != nil {
		return fmt.Errorf("load execution security snapshot: %w", err)
	}
	if !reflect.DeepEqual(security, value.Input.Security) {
		return errors.New("execution security snapshot does not match execution input")
	}
	return nil
}

func (s *Store) SaveAgentExecution(ctx context.Context, value executionmodel.AgentExecution) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if !sqlite.HasTransaction(ctx) {
		return errors.New("product mutation requires sqlite transaction")
	}
	if err := s.validateExecutionMutation(ctx, value); err != nil {
		return err
	}
	documentRef, err := s.putDocument(ctx, "execution", value.ID.String(), value)
	if err != nil {
		return err
	}
	err = s.saveMetadata(ctx, `INSERT INTO agent_executions (
		id, session_id, agent_id, request_id, work_item_id, parent_execution_id,
		context_revision, context_digest, security_policy_revision, security_fingerprint,
		reason, status, outcome, created_at, started_at, settled_at,
		start_content_digest, document_ref
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		session_id = excluded.session_id, agent_id = excluded.agent_id, request_id = excluded.request_id,
		work_item_id = excluded.work_item_id, parent_execution_id = excluded.parent_execution_id,
		context_revision = excluded.context_revision, context_digest = excluded.context_digest,
		security_policy_revision = excluded.security_policy_revision,
		security_fingerprint = excluded.security_fingerprint, reason = excluded.reason, status = excluded.status,
		outcome = excluded.outcome, created_at = excluded.created_at, started_at = excluded.started_at,
		settled_at = excluded.settled_at, start_content_digest = excluded.start_content_digest,
		document_ref = excluded.document_ref`,
		value.ID.String(), value.SessionID.String(), value.AgentID.String(), value.RequestID.String(),
		value.WorkItemID.String(), value.ParentExecutionID.String(), value.ContextRevision,
		value.Input.ContextDigest,
		value.Input.Security.AgentPolicyRevision, value.Input.Security.Fingerprint,
		string(value.Reason), string(value.Status), string(value.Outcome),
		value.CreatedAt.UTC().Format(time.RFC3339Nano), nullableTimeValue(value.StartedAt),
		nullableTimeValue(value.SettledAt), value.StartContentDigest, documentRef)
	if err != nil && isConstraintError(err, "agent_executions.agent_id, agent_executions.request_id") {
		return fmt.Errorf("%w: %s", contracts.ErrRequestConflict, value.RequestID)
	}
	if err != nil && isConstraintError(err, "agent_executions.agent_id") {
		return fmt.Errorf("%w: %s", contracts.ErrAgentExecuting, value.AgentID)
	}
	if err != nil {
		return err
	}
	return s.saveExecutionSecuritySnapshot(ctx, value)
}

func (s *Store) validateExecutionMutation(ctx context.Context, value executionmodel.AgentExecution) error {
	row := s.executor(ctx).QueryRowContext(ctx, `SELECT document_ref FROM agent_executions WHERE id = ?`, value.ID.String())
	var documentRef string
	if err := row.Scan(&documentRef); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read existing agent execution: %w", err)
	}
	var existing executionmodel.AgentExecution
	if err := s.loadDocument(ctx, documentRef, &existing, "agent execution", nil); err != nil {
		return fmt.Errorf("decode existing agent execution: %w", err)
	}
	if existing.SessionID != value.SessionID || existing.AgentID != value.AgentID ||
		existing.RequestID != value.RequestID || existing.WorkItemID != value.WorkItemID ||
		existing.ParentExecutionID != value.ParentExecutionID || existing.ContextRevision != value.ContextRevision ||
		existing.Reason != value.Reason || !existing.CreatedAt.Equal(value.CreatedAt) ||
		!reflect.DeepEqual(existing.Input, value.Input) {
		return errors.New("agent execution identity or input is immutable")
	}
	return nil
}

func (s *Store) GetExecutionSecuritySnapshot(ctx context.Context, id contracts.AgentExecutionID) (contracts.ExecutionSecuritySnapshot, error) {
	row := s.executor(ctx).QueryRowContext(ctx,
		`SELECT agent_policy_revision, fingerprint, document_ref FROM execution_security_snapshots WHERE execution_id = ?`, id.String())
	var revision uint64
	var fingerprint, documentRef string
	if err := row.Scan(&revision, &fingerprint, &documentRef); errors.Is(err, sql.ErrNoRows) {
		return contracts.ExecutionSecuritySnapshot{}, contracts.ErrNotFound
	} else if err != nil {
		return contracts.ExecutionSecuritySnapshot{}, fmt.Errorf("read execution security snapshot: %w", err)
	}
	var value contracts.ExecutionSecuritySnapshot
	if err := s.loadDocument(ctx, documentRef, &value, "execution security snapshot", func() error { return value.Validate() }); err != nil {
		return contracts.ExecutionSecuritySnapshot{}, err
	}
	if value.AgentPolicyRevision != revision || value.Fingerprint != fingerprint {
		return contracts.ExecutionSecuritySnapshot{}, errors.New("execution security snapshot index does not match document")
	}
	return value, nil
}

func (s *Store) saveExecutionSecuritySnapshot(ctx context.Context, execution executionmodel.AgentExecution) error {
	documentRef, err := s.putDocument(ctx, "execution-security", execution.ID.String(), execution.Input.Security)
	if err != nil {
		return err
	}
	result, err := s.executor(ctx).ExecContext(ctx, `INSERT INTO execution_security_snapshots (
		execution_id, agent_policy_revision, fingerprint, document_ref
	) VALUES (?, ?, ?, ?) ON CONFLICT(execution_id) DO NOTHING`,
		execution.ID.String(), execution.Input.Security.AgentPolicyRevision,
		execution.Input.Security.Fingerprint, documentRef)
	if err != nil {
		return fmt.Errorf("persist execution security snapshot: %w", err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("check execution security snapshot: %w", err)
	}
	if affected != 0 {
		return nil
	}
	existing, err := s.GetExecutionSecuritySnapshot(ctx, execution.ID)
	if err != nil {
		return err
	}
	if !reflect.DeepEqual(existing, execution.Input.Security) {
		return errors.New("execution security snapshot is immutable")
	}
	return nil
}

func (s *Store) FindExecutionByRequest(ctx context.Context, agentID contracts.AgentID, requestID contracts.RequestID) (executionmodel.AgentExecution, error) {
	return s.loadExecutionByQuery(ctx, `SELECT context_revision, context_digest, document_ref FROM agent_executions WHERE agent_id = ? AND request_id = ?`, []any{agentID.String(), requestID.String()}, "agent execution request")
}

func (s *Store) GetActiveExecutionByAgent(ctx context.Context, agentID contracts.AgentID) (executionmodel.AgentExecution, error) {
	return s.loadExecutionByQuery(ctx, `SELECT context_revision, context_digest, document_ref FROM agent_executions WHERE agent_id = ? AND status IN ('starting', 'running', 'settling')`, []any{agentID.String()}, "active agent execution")
}

func (s *Store) loadExecutionByQuery(ctx context.Context, query string, args []any, name string) (executionmodel.AgentExecution, error) {
	var revision uint64
	var digest, documentRef string
	if err := s.executor(ctx).QueryRowContext(ctx, query, args...).Scan(&revision, &digest, &documentRef); errors.Is(err, sql.ErrNoRows) {
		return executionmodel.AgentExecution{}, contracts.ErrNotFound
	} else if err != nil {
		return executionmodel.AgentExecution{}, fmt.Errorf("read %s: %w", name, err)
	}
	var value executionmodel.AgentExecution
	if err := s.loadDocument(ctx, documentRef, &value, name, func() error { return value.Validate() }); err != nil {
		return executionmodel.AgentExecution{}, err
	}
	if value.ContextRevision != revision || value.Input.ContextDigest != digest {
		return executionmodel.AgentExecution{}, errors.New("agent execution context index does not match document")
	}
	if err := s.validateStoredExecution(ctx, value); err != nil {
		return executionmodel.AgentExecution{}, err
	}
	return value, nil
}

func (s *Store) ListAgentExecutionsByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]executionmodel.AgentExecution, error) {
	return s.listExecutions(ctx,
		`SELECT context_revision, context_digest, document_ref FROM agent_executions WHERE agent_id = ? ORDER BY created_at DESC, id DESC LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)}, "agent executions")
}

func (s *Store) CountActiveExecutionsBySession(ctx context.Context, sessionID contracts.SessionID) (int, error) {
	var count int
	if err := s.executor(ctx).QueryRowContext(ctx, `SELECT COUNT(*) FROM agent_executions WHERE session_id = ? AND status IN ('starting', 'running', 'settling')`, sessionID.String()).Scan(&count); err != nil {
		return 0, fmt.Errorf("count active executions for session: %w", err)
	}
	return count, nil
}

func (s *Store) listExecutions(ctx context.Context, query string, args []any, name string) ([]executionmodel.AgentExecution, error) {
	rows, err := s.executor(ctx).QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list %s: %w", name, err)
	}
	defer rows.Close()
	values := make([]executionmodel.AgentExecution, 0)
	for rows.Next() {
		var revision uint64
		var digest, documentRef string
		if err := rows.Scan(&revision, &digest, &documentRef); err != nil {
			return nil, fmt.Errorf("scan %s: %w", name, err)
		}
		var value executionmodel.AgentExecution
		if err := s.loadDocument(ctx, documentRef, &value, name, func() error { return value.Validate() }); err != nil {
			return nil, err
		}
		if value.ContextRevision != revision || value.Input.ContextDigest != digest {
			return nil, errors.New("agent execution context index does not match document")
		}
		values = append(values, value)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate %s: %w", name, err)
	}
	return values, nil
}

type AgentExecutionRepository struct{ store *Store }
type ExecutionSecuritySnapshotRepository struct{ store *Store }

func (r ExecutionSecuritySnapshotRepository) Get(ctx context.Context, id contracts.AgentExecutionID) (contracts.ExecutionSecuritySnapshot, error) {
	return r.store.GetExecutionSecuritySnapshot(ctx, id)
}
func (r AgentExecutionRepository) Get(ctx context.Context, id contracts.AgentExecutionID) (executionmodel.AgentExecution, error) {
	return r.store.GetAgentExecution(ctx, id)
}
func (r AgentExecutionRepository) Save(ctx context.Context, value executionmodel.AgentExecution) error {
	return r.store.SaveAgentExecution(ctx, value)
}
func (r AgentExecutionRepository) FindByRequest(ctx context.Context, agentID contracts.AgentID, requestID contracts.RequestID) (executionmodel.AgentExecution, error) {
	return r.store.FindExecutionByRequest(ctx, agentID, requestID)
}
func (r AgentExecutionRepository) GetActiveByAgent(ctx context.Context, agentID contracts.AgentID) (executionmodel.AgentExecution, error) {
	return r.store.GetActiveExecutionByAgent(ctx, agentID)
}
func (r AgentExecutionRepository) ListByAgent(ctx context.Context, agentID contracts.AgentID, limit int) ([]executionmodel.AgentExecution, error) {
	return r.store.ListAgentExecutionsByAgent(ctx, agentID, limit)
}
func (r AgentExecutionRepository) CountActiveBySession(ctx context.Context, sessionID contracts.SessionID) (int, error) {
	return r.store.CountActiveExecutionsBySession(ctx, sessionID)
}
