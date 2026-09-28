package storage

import (
	"praxis/internal/contracts"
	toolmodel "praxis/internal/tool_invocation"

	"context"
	"database/sql"
	"errors"
	"reflect"
	"time"

	"praxis/internal/infra/sqlite"
	"praxis/internal/repository"
)

func (s *Store) GetToolInvocation(ctx context.Context, id contracts.ToolInvocationID) (toolmodel.ToolInvocation, error) {
	return loadDocumentRef[toolmodel.ToolInvocation](ctx, s, `SELECT document_ref FROM tool_invocations WHERE id = ?`, []any{id.String()}, "tool invocation", func(value toolmodel.ToolInvocation) error { return value.Validate() })
}
func (s *Store) FindToolInvocationByExecutionCall(ctx context.Context, executionID contracts.AgentExecutionID, providerToolCallID string) (toolmodel.ToolInvocation, error) {
	return loadDocumentRef[toolmodel.ToolInvocation](ctx, s, `SELECT document_ref FROM tool_invocations WHERE execution_id = ? AND provider_tool_call_id = ?`, []any{executionID.String(), providerToolCallID}, "tool invocation identity", func(value toolmodel.ToolInvocation) error { return value.Validate() })
}
func (s *Store) SaveToolInvocation(ctx context.Context, value toolmodel.ToolInvocation) error {
	if err := value.Validate(); err != nil {
		return err
	}
	if !sqlite.HasTransaction(ctx) {
		return errors.New("tool invocation mutation requires sqlite transaction")
	}
	var existing toolmodel.ToolInvocation
	row := s.executor(ctx).QueryRowContext(ctx, `SELECT document_ref FROM tool_invocations WHERE id = ?`, value.ID.String())
	var ref string
	if err := row.Scan(&ref); err == nil {
		if err := s.loadDocument(ctx, ref, &existing, "tool invocation", func() error { return existing.Validate() }); err != nil {
			return err
		}
		if existing.ExecutionID != value.ExecutionID || existing.SessionID != value.SessionID || existing.AgentID != value.AgentID || existing.ProviderToolCallID != value.ProviderToolCallID || existing.Name != value.Name || existing.ArgumentsDigest != value.ArgumentsDigest || !existing.CreatedAt.Equal(value.CreatedAt) || !reflect.DeepEqual(existing.NormalizedArguments, value.NormalizedArguments) {
			return errors.New("tool invocation identity or normalized arguments are immutable")
		}
	} else if err != sql.ErrNoRows {
		return err
	}
	documentRef, err := s.putDocument(ctx, "tool-invocation", value.ID.String(), value)
	if err != nil {
		return err
	}
	err = s.saveMetadata(ctx, `INSERT INTO tool_invocations (id, execution_id, session_id, agent_id, provider_tool_call_id, name, arguments_digest, status, failure_code, created_at, approved_at, started_at, settled_at, document_ref) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET status = excluded.status, failure_code = excluded.failure_code, approved_at = excluded.approved_at, started_at = excluded.started_at, settled_at = excluded.settled_at, document_ref = excluded.document_ref`, value.ID.String(), value.ExecutionID.String(), value.SessionID.String(), value.AgentID.String(), value.ProviderToolCallID, string(value.Name), value.ArgumentsDigest, string(value.Status), string(value.FailureCode), value.CreatedAt.UTC().Format(time.RFC3339Nano), nullableTimeValue(value.ApprovedAt), nullableTimeValue(value.StartedAt), nullableTimeValue(value.SettledAt), documentRef)
	if err != nil && isConstraintError(err, "tool_invocations.execution_id, tool_invocations.provider_tool_call_id") {
		return contracts.ErrRequestConflict
	}
	return err
}

type ToolInvocationRepository struct{ store *Store }

var _ repository.ToolInvocationRepository = ToolInvocationRepository{}

func (r ToolInvocationRepository) Get(ctx context.Context, id contracts.ToolInvocationID) (toolmodel.ToolInvocation, error) {
	return r.store.GetToolInvocation(ctx, id)
}
func (r ToolInvocationRepository) FindByExecutionCall(ctx context.Context, id contracts.AgentExecutionID, callID string) (toolmodel.ToolInvocation, error) {
	return r.store.FindToolInvocationByExecutionCall(ctx, id, callID)
}
func (r ToolInvocationRepository) Save(ctx context.Context, value toolmodel.ToolInvocation) error {
	return r.store.SaveToolInvocation(ctx, value)
}
