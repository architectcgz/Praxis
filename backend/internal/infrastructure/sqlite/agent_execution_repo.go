package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
)

func (s *Store) GetAgentExecution(
	ctx context.Context,
	id domainfoundation.AgentExecutionID,
) (domainexecution.AgentExecution, error) {
	var value domainexecution.AgentExecution
	if err := s.loadPayload(ctx, "agent_executions", id.String(), &value, func() error { return value.Validate() }); err != nil {
		return value, err
	}
	if err := s.validateStoredExecution(ctx, value); err != nil {
		return value, err
	}
	return value, nil
}

func (s *Store) validateStoredExecution(ctx context.Context, value domainexecution.AgentExecution) error {
	if err := value.Validate(); err != nil {
		return err
	}
	security, err := s.GetExecutionSecuritySnapshot(ctx, value.ID)
	if err != nil {
		return fmt.Errorf("load execution security snapshot: %w", err)
	}
	stored, err := encodePayload(security)
	if err != nil {
		return err
	}
	embedded, err := encodePayload(value.Input.Security)
	if err != nil {
		return err
	}
	if !bytes.Equal(stored, embedded) {
		return errors.New("execution security snapshot does not match execution input")
	}
	return nil
}

func (s *Store) SaveAgentExecution(ctx context.Context, value domainexecution.AgentExecution) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if err := s.validateExecutionMutation(ctx, value); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(
		ctx,
		`INSERT INTO agent_executions (
			id, session_id, agent_id, request_id, work_item_id, parent_execution_id,
			context_revision, security_policy_revision, security_fingerprint,
			reason, status, outcome, created_at, started_at, settled_at, payload
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			session_id = excluded.session_id, agent_id = excluded.agent_id, request_id = excluded.request_id,
			work_item_id = excluded.work_item_id, parent_execution_id = excluded.parent_execution_id,
			context_revision = excluded.context_revision, security_policy_revision = excluded.security_policy_revision,
			security_fingerprint = excluded.security_fingerprint, reason = excluded.reason, status = excluded.status,
			outcome = excluded.outcome, created_at = excluded.created_at, started_at = excluded.started_at,
			settled_at = excluded.settled_at, payload = excluded.payload`,
		value.ID.String(), value.SessionID.String(), value.AgentID.String(), value.RequestID.String(),
		value.WorkItemID.String(), value.ParentExecutionID.String(), value.ContextRevision,
		value.Input.Security.AgentPolicyRevision, value.Input.Security.Fingerprint,
		string(value.Reason), string(value.Status), string(value.Outcome),
		nullableTimeValue(value.CreatedAt),
		nullableTimeValue(value.StartedAt),
		nullableTimeValue(value.SettledAt),
		payload,
	)
	if err != nil && isConstraintError(err, "agent_executions.agent_id, agent_executions.request_id") {
		return fmt.Errorf("%w: %s", domainfoundation.ErrRequestConflict, value.RequestID)
	}
	if err != nil && isConstraintError(err, "agent_executions.agent_id") {
		return fmt.Errorf("%w: %s", domainfoundation.ErrAgentExecuting, value.AgentID)
	}
	if err != nil {
		return err
	}
	return s.saveExecutionSecuritySnapshot(ctx, value)
}

// validateExecutionMutation protects the immutable execution identity and
// input before the lifecycle row is upserted. StartContent is intentionally
// excluded because it is cleared after the transcript input receipt is synced.
func (s *Store) validateExecutionMutation(ctx context.Context, value domainexecution.AgentExecution) error {
	row := executorFromContext(ctx, s.db).QueryRowContext(ctx,
		`SELECT payload FROM agent_executions WHERE id = ?`, value.ID.String())
	var payload []byte
	if err := row.Scan(&payload); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read existing agent execution: %w", err)
	}
	var existing domainexecution.AgentExecution
	if err := json.Unmarshal(payload, &existing); err != nil {
		return fmt.Errorf("decode existing agent execution: %w", err)
	}
	existingInput, err := encodePayload(existing.Input)
	if err != nil {
		return fmt.Errorf("encode existing agent execution input: %w", err)
	}
	currentInput, err := encodePayload(value.Input)
	if err != nil {
		return fmt.Errorf("encode agent execution input: %w", err)
	}
	if existing.SessionID != value.SessionID || existing.AgentID != value.AgentID ||
		existing.RequestID != value.RequestID || existing.WorkItemID != value.WorkItemID ||
		existing.ParentExecutionID != value.ParentExecutionID || existing.ContextRevision != value.ContextRevision ||
		existing.Reason != value.Reason || !existing.CreatedAt.Equal(value.CreatedAt) ||
		!bytes.Equal(existingInput, currentInput) {
		return errors.New("agent execution identity or input is immutable")
	}
	return nil
}

// GetExecutionSecuritySnapshot reads the immutable authorization value stored
// separately from the mutable execution lifecycle row.
func (s *Store) GetExecutionSecuritySnapshot(ctx context.Context, id domainfoundation.AgentExecutionID) (domainsecurity.ExecutionSecuritySnapshot, error) {
	row := executorFromContext(ctx, s.db).QueryRowContext(ctx,
		`SELECT agent_policy_revision, fingerprint, payload FROM execution_security_snapshots WHERE execution_id = ?`, id.String())
	var revision uint64
	var fingerprint string
	var payload []byte
	if err := row.Scan(&revision, &fingerprint, &payload); errors.Is(err, sql.ErrNoRows) {
		return domainsecurity.ExecutionSecuritySnapshot{}, domainfoundation.ErrNotFound
	} else if err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, fmt.Errorf("read execution security snapshot: %w", err)
	}
	var value domainsecurity.ExecutionSecuritySnapshot
	if err := json.Unmarshal(payload, &value); err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, fmt.Errorf("decode execution security snapshot: %w", err)
	}
	if err := value.Validate(); err != nil {
		return domainsecurity.ExecutionSecuritySnapshot{}, fmt.Errorf("validate execution security snapshot: %w", err)
	}
	if value.AgentPolicyRevision != revision || value.Fingerprint != fingerprint {
		return domainsecurity.ExecutionSecuritySnapshot{}, errors.New("execution security snapshot index does not match payload")
	}
	return value, nil
}

func (s *Store) saveExecutionSecuritySnapshot(ctx context.Context, execution domainexecution.AgentExecution) error {
	payload, err := encodePayload(execution.Input.Security)
	if err != nil {
		return err
	}
	result, err := executorFromContext(ctx, s.db).ExecContext(ctx,
		`INSERT INTO execution_security_snapshots (
			execution_id, agent_policy_revision, fingerprint, payload
		) VALUES (?, ?, ?, ?)
		ON CONFLICT(execution_id) DO NOTHING`,
		execution.ID.String(), execution.Input.Security.AgentPolicyRevision,
		execution.Input.Security.Fingerprint, payload)
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
	existingPayload, err := encodePayload(existing)
	if err != nil {
		return err
	}
	if !bytes.Equal(existingPayload, payload) {
		return errors.New("execution security snapshot is immutable")
	}
	return nil
}

func (s *Store) FindExecutionByRequest(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	requestID domainfoundation.RequestID,
) (domainexecution.AgentExecution, error) {
	return loadTargetPayload[domainexecution.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions WHERE agent_id = ? AND request_id = ?`,
		[]any{agentID.String(), requestID.String()},
		"agent execution request",
		func(value domainexecution.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) GetActiveExecutionByAgent(
	ctx context.Context,
	agentID domainfoundation.AgentID,
) (domainexecution.AgentExecution, error) {
	return loadTargetPayload[domainexecution.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions
		 WHERE agent_id = ? AND status IN ('starting', 'running', 'settling')`,
		[]any{agentID.String()},
		"active agent execution",
		func(value domainexecution.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) ListAgentExecutionsByAgent(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	limit int,
) ([]domainexecution.AgentExecution, error) {
	return listTargetPayloads[domainexecution.AgentExecution](
		ctx,
		s,
		`SELECT payload FROM agent_executions WHERE agent_id = ?
		 ORDER BY created_at DESC, id DESC LIMIT ?`,
		[]any{agentID.String(), targetLimit(limit)},
		"agent executions",
		func(value domainexecution.AgentExecution) error { return value.Validate() },
	)
}

func (s *Store) CountActiveExecutionsBySession(ctx context.Context, sessionID domainfoundation.SessionID) (int, error) {
	var count int
	err := executorFromContext(ctx, s.db).QueryRowContext(
		ctx,
		`SELECT COUNT(*) FROM agent_executions execution
		 WHERE execution.session_id = ? AND execution.status IN ('starting', 'running', 'settling')`,
		sessionID.String(),
	).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count active executions for session: %w", err)
	}
	return count, nil
}

func (s *Store) ListRecoverableExecutionsAfter(ctx context.Context, after time.Time, afterID domainfoundation.AgentExecutionID, limit int) ([]domainexecution.AgentExecution, error) {
	afterValue := ""
	if !after.IsZero() {
		afterValue = after.UTC().Format(time.RFC3339Nano)
	}
	return listTargetPayloads[domainexecution.AgentExecution](ctx, s,
		`SELECT payload FROM agent_executions WHERE status IN ('starting', 'running', 'settling') AND (created_at > ? OR (created_at = ? AND id > ?)) ORDER BY created_at, id LIMIT ?`,
		[]any{afterValue, afterValue, afterID.String(), targetLimit(limit)}, "recoverable agent executions", func(value domainexecution.AgentExecution) error { return value.Validate() })
}

func (s *Store) ListStartingExecutionsAfter(ctx context.Context, after time.Time, afterID domainfoundation.AgentExecutionID, limit int) ([]domainexecution.AgentExecution, error) {
	afterValue := ""
	if !after.IsZero() {
		afterValue = after.UTC().Format(time.RFC3339Nano)
	}
	return listTargetPayloads[domainexecution.AgentExecution](ctx, s,
		`SELECT payload FROM agent_executions WHERE status = 'starting' AND (created_at > ? OR (created_at = ? AND id > ?)) ORDER BY created_at, id LIMIT ?`,
		[]any{afterValue, afterValue, afterID.String(), targetLimit(limit)}, "starting agent executions", func(value domainexecution.AgentExecution) error { return value.Validate() })
}

type AgentExecutionRepository struct{ store *Store }

type ExecutionSecuritySnapshotRepository struct{ store *Store }

func (r ExecutionSecuritySnapshotRepository) Get(ctx context.Context, id domainfoundation.AgentExecutionID) (domainsecurity.ExecutionSecuritySnapshot, error) {
	return r.store.GetExecutionSecuritySnapshot(ctx, id)
}

func (r AgentExecutionRepository) Get(
	ctx context.Context,
	id domainfoundation.AgentExecutionID,
) (domainexecution.AgentExecution, error) {
	return r.store.GetAgentExecution(ctx, id)
}

func (r AgentExecutionRepository) Save(ctx context.Context, value domainexecution.AgentExecution) error {
	return r.store.SaveAgentExecution(ctx, value)
}

func (r AgentExecutionRepository) FindByRequest(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	requestID domainfoundation.RequestID,
) (domainexecution.AgentExecution, error) {
	return r.store.FindExecutionByRequest(ctx, agentID, requestID)
}

func (r AgentExecutionRepository) GetActiveByAgent(
	ctx context.Context,
	agentID domainfoundation.AgentID,
) (domainexecution.AgentExecution, error) {
	return r.store.GetActiveExecutionByAgent(ctx, agentID)
}

func (r AgentExecutionRepository) ListByAgent(
	ctx context.Context,
	agentID domainfoundation.AgentID,
	limit int,
) ([]domainexecution.AgentExecution, error) {
	return r.store.ListAgentExecutionsByAgent(ctx, agentID, limit)
}

func (r AgentExecutionRepository) CountActiveBySession(ctx context.Context, sessionID domainfoundation.SessionID) (int, error) {
	return r.store.CountActiveExecutionsBySession(ctx, sessionID)
}

func (r AgentExecutionRepository) ListRecoverableAfter(ctx context.Context, after time.Time, afterID domainfoundation.AgentExecutionID, limit int) ([]domainexecution.AgentExecution, error) {
	return r.store.ListRecoverableExecutionsAfter(ctx, after, afterID, limit)
}

func (r AgentExecutionRepository) ListStartingAfter(ctx context.Context, after time.Time, afterID domainfoundation.AgentExecutionID, limit int) ([]domainexecution.AgentExecution, error) {
	return r.store.ListStartingExecutionsAfter(ctx, after, afterID, limit)
}
