package jsonl

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/core/model"
	projectmodel "praxis/internal/core/project"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	workspacemodel "praxis/internal/core/workspace"
)

type storeFixture struct {
	root  string
	store *Store
	repos Repositories
	agent agentmodel.Agent
}

func requireOK(t testing.TB, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func newStoreFixture(t testing.TB) *storeFixture {
	t.Helper()
	root := t.TempDir()
	store, err := Open(context.Background(), filepath.Join(root, "runtime"))
	requireOK(t, err)
	f := &storeFixture{root: filepath.Join(root, "runtime"), store: store, repos: store.Repositories()}
	t.Cleanup(func() { _ = f.store.Close(context.Background()) })
	at := time.Now().UTC()
	project := projectmodel.Project{
		ID:                 "project",
		Name:               "项目",
		Path:               root,
		DefaultWorkspaceID: "workspace",
		State:              projectmodel.ProjectActive,
		CreatedAt:          at,
		UpdatedAt:          at,
	}
	workspace := workspacemodel.Workspace{
		ID:        "workspace",
		ProjectID: project.ID,
		Kind:      workspacemodel.WorkspaceProjectRoot,
		Path:      root,
		State:     workspacemodel.WorkspaceReady,
		Revision:  1,
		CreatedAt: at,
		UpdatedAt: at,
	}
	requireOK(t, store.InTx(context.Background(), func(ctx context.Context) error {
		// 引用方先写入，约束必须在整个事务的最终状态验证。
		if err := f.repos.Projects.Save(ctx, project); err != nil {
			return err
		}
		return f.repos.Workspaces.Save(ctx, workspace)
	}))
	session, err := sessionmodel.NewSession("session", "project", "workspace", at)
	requireOK(t, err)
	f.agent, err = agentmodel.NewAgent("agent", session.ID, agentmodel.DefinitionPrimary, contracts.ProfilePrimary, at)
	requireOK(t, err)
	requireOK(t, store.InTx(context.Background(), func(ctx context.Context) error {
		if err := f.repos.Agents.Save(ctx, f.agent); err != nil {
			return err
		}
		return f.repos.Sessions.Save(ctx, session)
	}))
	return f
}
func (f *storeFixture) restart(t testing.TB) {
	t.Helper()
	requireOK(t, f.store.Close(context.Background()))
	var err error
	f.store, err = Open(context.Background(), f.root)
	requireOK(t, err)
	f.repos = f.store.Repositories()
}
func (f *storeFixture) config() taskmodel.Config {
	return taskmodel.Config{
		AgentDefinitionID: agentmodel.DefinitionPrimary,
		WorkspacePath:     filepath.Dir(f.root),
		Model: model.Config{
			ProviderID:      "provider",
			ModelID:         "model",
			APIFormat:       "openai_chat",
			ContextWindow:   8192,
			MaxOutputTokens: 1024,
			BaseURL:         "https://example.com",
		},
	}
}
func (f *storeFixture) activeTask(t testing.TB) taskmodel.Task {
	t.Helper()
	at := time.Now().UTC()
	task, err := taskmodel.NewTask("task", "session", "agent", "request", "input:request", f.config(), taskmodel.ContextSelection{}, at)
	requireOK(t, err)
	requireOK(t, f.store.InTx(context.Background(), func(ctx context.Context) error {
		if _, err := f.repos.Messages.Append(ctx, f.agent, sessionmodel.MessageData{
			ID:         task.InputMessageID,
			TaskID:     task.ID.String(),
			RequestID:  task.RequestID.String(),
			Role:       sessionmodel.RoleUser,
			AuthorKind: sessionmodel.AuthorUser,
			Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "问题"}},
		}); err != nil {
			return err
		}
		if err := f.repos.Tasks.Save(ctx, task); err != nil {
			return err
		}
		agent := f.agent
		if err := agent.Start(task.ID, at); err != nil {
			return err
		}
		return f.repos.Agents.Save(ctx, agent)
	}))
	return task
}

func TestTransactionOverlayRollbackAndNested(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	sentinel := errors.New("rollback")
	var stale context.Context
	err := f.store.InTx(ctx, func(txCtx context.Context) error {
		stale = txCtx
		return f.store.InTx(txCtx, func(nested context.Context) error {
			v, err := f.repos.Projects.Get(nested, "project")
			if err != nil {
				return err
			}
			v.Name = "第一次"
			if err := f.repos.Projects.Save(nested, v); err != nil {
				return err
			}
			v.Name = "第二次"
			if err := f.repos.Projects.Save(nested, v); err != nil {
				return err
			}
			values, err := f.repos.Projects.List(nested, 0)
			if err != nil || len(values) != 1 || values[0].Name != v.Name {
				t.Fatalf("transaction did not read the final write: %+v, %v", values, err)
			}
			return sentinel
		})
	})
	if !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	v, err := f.repos.Projects.Get(ctx, "project")
	requireOK(t, err)
	if v.Name != "项目" {
		t.Fatal("rollback corrupted committed state")
	}
	if _, err := f.repos.Projects.Get(stale, "project"); err == nil {
		t.Fatal("ended transaction context must not be reused")
	}
	before := f.store.sequences[""]
	requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error { return f.repos.Projects.Save(txCtx, v) }))
	if f.store.sequences[""] != before {
		t.Fatal("idempotent write consumed a sequence number")
	}
}

func TestTransactionRejectsCrossScopeAndInvalidGraph(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	before := f.store.sequences[""]
	err := f.store.InTx(ctx, func(txCtx context.Context) error {
		p, err := f.repos.Projects.Get(txCtx, "project")
		if err != nil {
			return err
		}
		p.Name = "改变"
		if err := f.repos.Projects.Save(txCtx, p); err != nil {
			return err
		}
		session, err := sessionmodel.NewSession("second", "project", "workspace", time.Now())
		if err != nil {
			return err
		}
		return f.repos.Sessions.Save(txCtx, session)
	})
	if err == nil || f.store.sequences[""] != before {
		t.Fatal("cross-file transaction must roll back entirely")
	}
	err = f.store.InTx(ctx, func(txCtx context.Context) error {
		workspace, err := f.repos.Workspaces.Get(txCtx, "workspace")
		if err != nil {
			return err
		}
		workspace.ProjectID = "missing"
		return f.repos.Workspaces.Save(txCtx, workspace)
	})
	if err == nil {
		t.Fatal("invalid references must be rejected")
	}
}

func TestUniqueAndMonotonicConstraints(t *testing.T) {
	f := newStoreFixture(t)
	task := f.activeTask(t)
	ctx := context.Background()
	for _, mutate := range []func(*taskmodel.Task){
		func(v *taskmodel.Task) { v.ID = "other" },
		func(v *taskmodel.Task) { v.ID = "other"; v.RequestID = "another" },
		func(v *taskmodel.Task) { v.Config.SystemPrompt = "changed" },
	} {
		err := f.store.InTx(ctx, func(txCtx context.Context) error { next := task; mutate(&next); return f.repos.Tasks.Save(txCtx, next) })
		if err == nil {
			t.Fatal("uniqueness or frozen configuration constraint was not enforced")
		}
	}
	requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error {
		task.Progress.OutputBytes = 10
		return f.repos.Tasks.Save(txCtx, task)
	}))
	err := f.store.InTx(ctx, func(txCtx context.Context) error {
		task.Progress.OutputBytes = 1
		return f.repos.Tasks.Save(txCtx, task)
	})
	if err == nil {
		t.Fatal("budget must not regress")
	}
}
