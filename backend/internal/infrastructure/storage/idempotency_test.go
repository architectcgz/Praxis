package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	domaincontext "praxis/internal/domain/context"
	domainfoundation "praxis/internal/domain/foundation"
	domainsecurity "praxis/internal/domain/security"
	domainworkflow "praxis/internal/domain/workflow"
	"praxis/internal/infrastructure/document"
	"praxis/internal/infrastructure/sqlite"
)

func seedIdentity(t *testing.T, db *sqlite.Store, at time.Time) {
	t.Helper()
	stamp := at.UTC().Format(time.RFC3339Nano)
	if err := db.InTx(context.Background(), func(ctx context.Context) error {
		statements := []string{
			`INSERT INTO projects (id, name, default_workspace_id, path, state, created_at, updated_at) VALUES ('project_1', 'project', 'workspace_1', '/tmp/praxis', 'active', ?, ?)`,
			`INSERT INTO workspaces (id, project_id, kind, path, state, revision, created_at, updated_at) VALUES ('workspace_1', 'project_1', 'project_root', '/tmp/praxis', 'ready', 1, ?, ?)`,
			`INSERT INTO sessions (id, project_id, workspace_id, goal, state, created_at, updated_at) VALUES ('session_1', 'project_1', 'workspace_1', 'goal', 'active', ?, ?)`,
			`INSERT INTO agents (id, session_id, profile, security_policy_revision, state, current_execution_id, created_at, updated_at) VALUES ('agent_1', 'session_1', 'primary', 1, 'idle', '', ?, ?)`,
		}
		for _, statement := range statements {
			if _, err := db.Executor(ctx).ExecContext(ctx, statement, stamp, stamp); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
}

func TestSessionContextAppendIsIdempotentByEntryID(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	seedIdentity(t, db, time.Now())
	store, err := New(db, document.NewMemory())
	if err != nil {
		t.Fatal(err)
	}

	at := time.Now()
	entry, err := domaincontext.NewSessionContextEntry("context_1", "session_1", 1, domaincontext.SessionContextUserMessage, "", "hello", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(txCtx context.Context) error {
		return store.AppendSessionContext(txCtx, entry, 0)
	}); err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(txCtx context.Context) error {
		return store.AppendSessionContext(txCtx, entry, 0)
	}); err != nil {
		t.Fatalf("identical retry should be idempotent, got %v", err)
	}

	txCtx := ctx
	loaded, found, err := store.GetSessionContextEntryByID(txCtx, "context_1")
	if err != nil || !found || loaded.Content != "hello" {
		t.Fatalf("GetByID found=%v err=%v entry=%#v", found, err, loaded)
	}

	changed, err := domaincontext.NewSessionContextEntry("context_1", "session_1", 1, domaincontext.SessionContextUserMessage, "", "changed", at)
	if err != nil {
		t.Fatal(err)
	}
	err = db.InTx(ctx, func(txCtx context.Context) error {
		return store.AppendSessionContext(txCtx, changed, 0)
	})
	if !errors.Is(err, domainfoundation.ErrRequestConflict) {
		t.Fatalf("same entry id with different content should conflict, got %v", err)
	}
}

func TestAgentSecurityPolicyRevisionLookup(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	seedIdentity(t, db, time.Now())
	store, err := New(db, document.NewMemory())
	if err != nil {
		t.Fatal(err)
	}

	policy, err := domainsecurity.NewAgentSecurityPolicy(
		2,
		domainsecurity.CapabilityPolicy{ReadScopes: []string{"/tmp/praxis"}},
		domainsecurity.SandboxPolicy{Mode: domainsecurity.SandboxReadOnly},
		domainsecurity.ApprovalPolicy{Mode: domainsecurity.ApprovalAlwaysAsk},
	)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(txCtx context.Context) error {
		return store.SaveAgentSecurityPolicy(txCtx, "agent_1", policy)
	}); err != nil {
		t.Fatal(err)
	}
	loaded, found, err := store.GetAgentSecurityPolicy(ctx, "agent_1", 2)
	if err != nil || !found || loaded.Revision != 2 {
		t.Fatalf("revision 2 found=%v err=%v policy=%#v", found, err, loaded)
	}
	if _, found, err := store.GetAgentSecurityPolicy(ctx, "agent_1", 3); err != nil || found {
		t.Fatalf("revision 3 should be absent: found=%v err=%v", found, err)
	}
}

func TestContextDeliveryPersistsResultExecution(t *testing.T) {
	ctx := context.Background()
	db, err := sqlite.Open(ctx, ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close(ctx) }()
	seedIdentity(t, db, time.Now())

	at := time.Now()
	delivery, err := domainworkflow.NewContextDelivery("delivery_1", "session_1", "agent_result:result_1", "agent_1", "dedupe_1", at)
	if err != nil {
		t.Fatal(err)
	}
	if err := delivery.Begin(at); err != nil {
		t.Fatal(err)
	}
	if err := delivery.MarkDelivered("entry_1", "execution_1", at); err != nil {
		t.Fatal(err)
	}
	if err := db.InTx(ctx, func(txCtx context.Context) error {
		return db.SaveContextDelivery(txCtx, delivery)
	}); err != nil {
		t.Fatal(err)
	}
	loaded, err := db.GetContextDelivery(ctx, "delivery_1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ResultExecutionID != "execution_1" || loaded.ArtifactEntryRef != "entry_1" {
		t.Fatalf("unexpected delivery: %#v", loaded)
	}
}
