package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"testing"
	"time"

	domainexecution "praxis/internal/domain/execution"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"

	_ "modernc.org/sqlite"
)

func TestToolInvocationRepositoryPersistsLifecycle(t *testing.T) {
	ctx := context.Background()
	database, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if _, err := database.ExecContext(ctx, `CREATE TABLE tool_invocations (
		id TEXT PRIMARY KEY,
		execution_id TEXT NOT NULL,
		session_id TEXT NOT NULL,
		agent_id TEXT NOT NULL,
		provider_tool_call_id TEXT NOT NULL,
		name TEXT NOT NULL,
		arguments_digest TEXT NOT NULL,
		status TEXT NOT NULL,
		failure_code TEXT NOT NULL,
		created_at TEXT NOT NULL,
		approved_at TEXT,
		started_at TEXT,
		settled_at TEXT,
		payload TEXT NOT NULL,
		UNIQUE (execution_id, provider_tool_call_id)
	)`); err != nil {
		t.Fatal(err)
	}
	store, err := NewStore(database)
	if err != nil {
		t.Fatal(err)
	}
	repository := ToolInvocationRepository{store: store}
	createdAt := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	invocation, err := domainexecution.NewToolInvocation(
		"toolinvocation_test",
		"execution_test",
		"session_test",
		"agent_test",
		"provider-call-1",
		domainsecurity.ToolListDir,
		[]byte(`{"path":"C:\\workspace"}`),
		"digest_test",
		createdAt,
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := repository.Save(ctx, invocation); err == nil {
		t.Fatal("Save() outside a transaction succeeded")
	}
	if err := store.InTx(ctx, func(txCtx context.Context) error {
		return repository.Save(txCtx, invocation)
	}); err != nil {
		t.Fatal(err)
	}
	approval, err := domainsecurity.NewPolicyApproval("security_test", createdAt.Add(time.Second))
	if err != nil {
		t.Fatal(err)
	}
	if err := invocation.Approve(approval); err != nil {
		t.Fatal(err)
	}
	if err := invocation.Start(createdAt.Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	result := domainexecution.ToolInvocationResult{InlineContent: `{"entries":[]}`}
	if err := invocation.Succeed(result, createdAt.Add(3*time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := store.InTx(ctx, func(txCtx context.Context) error {
		return repository.Save(txCtx, invocation)
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := repository.FindByExecutionCall(ctx, invocation.ExecutionID, invocation.ProviderToolCallID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Status != domainexecution.ToolInvocationSucceeded ||
		loaded.Result.InlineContent != result.InlineContent || !loaded.SettledAt.Equal(invocation.SettledAt) {
		t.Fatalf("unexpected stored invocation: %#v", loaded)
	}
	if _, err := repository.Get(ctx, domainfoundation.ToolInvocationID("missing")); !errors.Is(err, domainfoundation.ErrNotFound) {
		t.Fatalf("Get(missing) error=%v", err)
	}
}
