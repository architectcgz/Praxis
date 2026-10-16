package compose

import (
	"context"
	"fmt"
	"os"
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
	"praxis/internal/request"
)

func TestDesktopPaginationKeepsCompleteHistory(t *testing.T) {
	root := bootstrapRoot(t)
	app, err := Open(context.Background(), root, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = app.Close(context.Background()) })
	ctx := context.Background()
	repos := app.store.Repositories()
	at := time.Now().UTC()
	workspacePath := t.TempDir()
	project, err := projectmodel.NewProject("project", "project", workspacePath, "workspace", at)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := workspacemodel.NewWorkspace("workspace", "project", workspacemodel.WorkspaceProjectRoot, workspacePath, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.InTx(ctx, func(ctx context.Context) error {
		if err := repos.Projects.Save(ctx, project); err != nil {
			return err
		}
		return repos.Workspaces.Save(ctx, workspace)
	}); err != nil {
		t.Fatal(err)
	}
	session, err := sessionmodel.NewSession("session", "project", "workspace", at)
	if err != nil {
		t.Fatal(err)
	}
	session.Title = "Mixed CASE title"
	agent, err := agentmodel.NewAgent("agent", "session", agentmodel.DefinitionPrimary, contracts.ProfilePrimary, at)
	if err != nil {
		t.Fatal(err)
	}
	if err := app.store.InTx(ctx, func(ctx context.Context) error {
		if err := repos.Sessions.Save(ctx, session); err != nil {
			return err
		}
		if err := repos.Agents.Save(ctx, agent); err != nil {
			return err
		}
		for i := range 150 {
			if _, err := repos.Messages.Append(ctx, agent, sessionmodel.MessageData{
				ID:         fmt.Sprintf("message-%d", i),
				Role:       sessionmodel.RoleUser,
				AuthorKind: sessionmodel.AuthorUser,
				Blocks:     []sessionmodel.Block{{Kind: sessionmodel.BlockText, Text: "history"}},
				CreatedAt:  at.Add(time.Duration(i) * time.Second),
			}); err != nil {
				return err
			}
			config := taskmodel.Config{
				AgentDefinitionID: agentmodel.DefinitionPrimary,
				WorkspacePath:     workspacePath,
				Model: model.Config{
					ProviderID:      "provider",
					ModelID:         "model",
					APIFormat:       "openai_chat",
					ContextWindow:   8192,
					MaxOutputTokens: 1024,
					BaseURL:         "https://example.com",
				},
			}
			task, err := taskmodel.NewTask(contracts.TaskID(fmt.Sprintf("task-%d", i)), session.ID, agent.ID, contracts.RequestID(fmt.Sprintf("request-%d", i)), fmt.Sprintf("message-%d", i), config, taskmodel.ContextSelection{}, at)
			if err != nil {
				return err
			}
			if err := task.MarkRunning(at); err != nil {
				return err
			}
			if err := task.End(taskmodel.TaskFailed, contracts.TaskFailureRuntimeFailed, at.Add(time.Duration(i)*time.Second)); err != nil {
				return err
			}
			if err := repos.Tasks.Save(ctx, task); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	tasks, err := repos.Tasks.ListByAgent(ctx, "agent", 0)
	if err != nil || len(tasks) != 150 {
		t.Fatal(len(tasks), err)
	}
	page, err := app.frontend.ListAgentHistory(ctx, "agent", "", 0)
	if err != nil || len(page.Items) != 100 || page.NextCursor == "" {
		t.Fatal(len(page.Items), page.NextCursor, err)
	}
	all, err := app.frontend.ListAgentHistory(ctx, "agent", "", 500)
	if err != nil || len(all.Items) != 300 {
		t.Fatal("history was truncated before pagination", len(all.Items), err)
	}
	second, err := app.frontend.ListAgentHistory(ctx, "agent", page.NextCursor, 100)
	if err != nil || len(second.Items) != 100 {
		t.Fatal(len(second.Items), err)
	}
	if _, err := app.frontend.ListAgentMessages(ctx, "agent", -1); err == nil {
		t.Fatal("negative limit accepted")
	}
	if _, err := app.frontend.ListProjects(ctx, -1); err == nil {
		t.Fatal("negative project limit accepted")
	}
	sessions, err := repos.Sessions.SearchTitle(ctx, "case", 0)
	if err != nil || len(sessions) != 1 {
		t.Fatal(sessions, err)
	}
	sessions, err = repos.Sessions.SearchTitle(ctx, " CASE ", 0)
	if err != nil || len(sessions) != 0 {
		t.Fatal("repository normalized query", sessions, err)
	}
	if err := os.WriteFile(filepath.Join(workspacePath, "large"), make([]byte, (1<<20)+1), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := app.frontend.PreviewFile(ctx, request.FilePreview{SessionID: "session", Path: "large"}); err == nil || err.Error() != "file exceeds the 1 MiB preview limit" {
		t.Fatal("preview did not map generic filesystem error", err)
	}
}
