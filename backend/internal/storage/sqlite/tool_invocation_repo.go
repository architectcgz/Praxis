package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	"praxis/internal/persistence"
)

func (s *Store) GetToolInvocation(
	ctx context.Context,
	id domainfoundation.ToolInvocationID,
) (domainexecution.ToolInvocation, error) {
	return loadTargetPayload[domainexecution.ToolInvocation](
		ctx, s, `SELECT payload FROM tool_invocations WHERE id = ?`, []any{id.String()},
		"tool invocation", func(value domainexecution.ToolInvocation) error { return value.Validate() },
	)
}

func (s *Store) FindToolInvocationByExecutionCall(
	ctx context.Context,
	executionID domainfoundation.AgentExecutionID,
	providerToolCallID string,
) (domainexecution.ToolInvocation, error) {
	return loadTargetPayload[domainexecution.ToolInvocation](
		ctx, s,
		`SELECT payload FROM tool_invocations WHERE execution_id = ? AND provider_tool_call_id = ?`,
		[]any{executionID.String(), providerToolCallID}, "tool invocation identity",
		func(value domainexecution.ToolInvocation) error { return value.Validate() },
	)
}

func (s *Store) SaveToolInvocation(ctx context.Context, value domainexecution.ToolInvocation) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if _, ok := contextTx(ctx); !ok {
		return errors.New("tool invocation mutation requires sqlite transaction")
	}
	if err := s.validateToolInvocationMutation(ctx, value); err != nil {
		return err
	}
	payload, err := encodePayload(value)
	if err != nil {
		return err
	}
	err = s.savePayload(ctx, `INSERT INTO tool_invocations (
		id, execution_id, session_id, agent_id, provider_tool_call_id, name,
		arguments_digest, status, failure_code, created_at, approved_at, started_at, settled_at, payload
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(id) DO UPDATE SET
		status = excluded.status, failure_code = excluded.failure_code,
		approved_at = excluded.approved_at, started_at = excluded.started_at,
		settled_at = excluded.settled_at, payload = excluded.payload`,
		value.ID.String(), value.ExecutionID.String(), value.SessionID.String(), value.AgentID.String(),
		value.ProviderToolCallID, string(value.Name), value.ArgumentsDigest, string(value.Status),
		string(value.FailureCode), nullableTimeValue(value.CreatedAt), nullableTimeValue(value.ApprovedAt),
		nullableTimeValue(value.StartedAt), nullableTimeValue(value.SettledAt), payload)
	if err != nil && isConstraintError(err, "tool_invocations.execution_id, tool_invocations.provider_tool_call_id") {
		return domainfoundation.ErrRequestConflict
	}
	return err
}

func (s *Store) validateToolInvocationMutation(ctx context.Context, value domainexecution.ToolInvocation) error {
	row := executorFromContext(ctx, s.db).QueryRowContext(
		ctx, `SELECT payload FROM tool_invocations WHERE id = ?`, value.ID.String(),
	)
	var payload []byte
	if err := row.Scan(&payload); errors.Is(err, sql.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("read existing tool invocation: %w", err)
	}
	var existing domainexecution.ToolInvocation
	if err := decodeStoredPayload(payload, &existing); err != nil {
		return fmt.Errorf("decode existing tool invocation: %w", err)
	}
	if err := existing.Validate(); err != nil {
		return fmt.Errorf("validate existing tool invocation: %w", err)
	}
	if existing.ExecutionID != value.ExecutionID || existing.SessionID != value.SessionID ||
		existing.AgentID != value.AgentID || existing.ProviderToolCallID != value.ProviderToolCallID ||
		existing.Name != value.Name || existing.ArgumentsDigest != value.ArgumentsDigest ||
		!existing.CreatedAt.Equal(value.CreatedAt) || !bytes.Equal(existing.NormalizedArguments, value.NormalizedArguments) {
		return errors.New("tool invocation identity or normalized arguments are immutable")
	}
	return nil
}

func decodeStoredPayload(payload []byte, target any) error {
	return json.Unmarshal(payload, target)
}

type ToolInvocationRepository struct{ store *Store }

var _ persistence.ToolInvocationRepository = ToolInvocationRepository{}

func (r ToolInvocationRepository) Get(
	ctx context.Context,
	id domainfoundation.ToolInvocationID,
) (domainexecution.ToolInvocation, error) {
	return r.store.GetToolInvocation(ctx, id)
}

func (r ToolInvocationRepository) FindByExecutionCall(
	ctx context.Context,
	executionID domainfoundation.AgentExecutionID,
	providerToolCallID string,
) (domainexecution.ToolInvocation, error) {
	return r.store.FindToolInvocationByExecutionCall(ctx, executionID, providerToolCallID)
}

func (r ToolInvocationRepository) Save(ctx context.Context, value domainexecution.ToolInvocation) error {
	return r.store.SaveToolInvocation(ctx, value)
}
