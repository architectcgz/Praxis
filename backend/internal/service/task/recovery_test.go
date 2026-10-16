package task

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/infra/jsonl"
)

func (f *hookFixture) restart(t *testing.T) {
	t.Helper()
	if err := f.store.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	store, err := jsonl.Open(context.Background(), f.root)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(context.Background()) })
	f.store, f.repos = store, store.Repositories()
	f.svc.tx = store
	f.svc.controls = f.repos.Controls
	f.svc.workspaces = f.repos.Workspaces
	f.svc.sessions = f.repos.Sessions
	f.svc.agents = f.repos.Agents
	f.svc.tasks = f.repos.Tasks
	f.svc.messages = f.repos.Messages
	f.svc.contexts = f.repos.Contexts
	f.svc.invocations = f.repos.ToolInvocations
}

func addRecoveryCalls(t *testing.T, f *hookFixture, task taskmodel.Task) {
	t.Helper()
	ctx := context.Background()
	writer, err := f.repos.Messages.Writer(ctx, task.SessionID, task.AgentID)
	if err != nil {
		t.Fatal(err)
	}
	turn := contracts.NewTurnID(task.ID, 1)
	_, _, err = writer.AppendAssistant(ctx, testAssistantWrite(t, sessionmodel.MessageData{
		ID:         "assistant:" + turn.String(),
		RequestID:  task.RequestID.String(),
		TaskID:     task.ID.String(),
		TurnID:     turn.String(),
		Role:       sessionmodel.RoleAssistant,
		AuthorKind: sessionmodel.AuthorAgent,
		AuthorID:   task.AgentID.String(),
		StopReason: "tool_calls",
		Blocks: []sessionmodel.Block{
			{
				Kind:   sessionmodel.BlockToolCall,
				CallID: "z-first",
				Name:   "read_file",
				Input:  []byte(`{}`),
			},
			{
				Kind:   sessionmodel.BlockToolCall,
				CallID: "a-second",
				Name:   "read_file",
				Input:  []byte(`{}`),
			},
		},
	}, contracts.ToolInvocationContext{
		SessionID:     task.SessionID,
		AgentID:       task.AgentID,
		TaskID:        task.ID,
		TurnID:        turn,
		WorkspacePath: task.Config.WorkspacePath,
	}))
	if err != nil {
		t.Fatal(err)
	}
}

func setRecoveryStatus(t *testing.T, f *hookFixture, task taskmodel.Task, status toolmodel.ToolInvocationStatus) {
	t.Helper()
	err := f.store.InTx(context.Background(), func(ctx context.Context) error {
		for _, call := range []string{"z-first", "a-second"} {
			inv, err := f.repos.ToolInvocations.FindByTaskCall(ctx, task.ID, call)
			if err != nil {
				return err
			}
			if status == toolmodel.ToolInvocationRequested {
				continue
			}
			inv.NormalizedArguments = []byte(`{}`)
			if status == toolmodel.ToolInvocationAwaitingApproval {
				if err := inv.AwaitApproval(); err != nil {
					return err
				}
			} else {
				approval, err := contracts.NewPolicyApproval(toolmodel.ArgumentsDigest(inv.Name, inv.NormalizedArguments), time.Now())
				if err != nil {
					return err
				}
				if err := inv.Approve(approval); err != nil {
					return err
				}
				if err := f.repos.ToolInvocations.Save(ctx, inv); err != nil {
					return err
				}
				if status == toolmodel.ToolInvocationRunning || status == toolmodel.ToolInvocationSucceeded {
					if err := inv.Start(time.Now()); err != nil {
						return err
					}
					if err := f.repos.ToolInvocations.Save(ctx, inv); err != nil {
						return err
					}
				}
				if status == toolmodel.ToolInvocationSucceeded {
					if err := inv.Succeed(toolmodel.ToolInvocationResult{InlineContent: call}, time.Now()); err != nil {
						return err
					}
				}
			}
			if err := f.repos.ToolInvocations.Save(ctx, inv); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestStartupToolRecoveryMatrix(t *testing.T) {
	for _, status := range []toolmodel.ToolInvocationStatus{
		toolmodel.ToolInvocationRequested,
		toolmodel.ToolInvocationApproved,
		toolmodel.ToolInvocationAwaitingApproval,
		toolmodel.ToolInvocationRunning,
		toolmodel.ToolInvocationSucceeded,
	} {
		for _, ended := range []bool{false, true} {
			t.Run(string(status)+map[bool]string{false: "/active", true: "/ended"}[ended], func(t *testing.T) {
				f := newHookFixture(t)
				ctx := context.Background()
				created, err := f.svc.SendInput(ctx, hookInput())
				if err != nil {
					t.Fatal(err)
				}
				task := created.Task
				addRecoveryCalls(t, f, task)
				setRecoveryStatus(t, f, task, status)
				if ended {
					err = f.store.InTx(ctx, func(txCtx context.Context) error {
						if err := task.MarkRunning(time.Now()); err != nil {
							return err
						}
						if err := task.End(taskmodel.TaskInterrupted, contracts.TaskFailureInterrupted, time.Now()); err != nil {
							return err
						}
						if err := f.repos.Tasks.Save(txCtx, task); err != nil {
							return err
						}
						agent, err := f.repos.Agents.Get(txCtx, task.AgentID)
						if err != nil {
							return err
						}
						if err := agent.EndTask(task.Outcome, time.Now()); err != nil {
							return err
						}
						return f.repos.Agents.Save(txCtx, agent)
					})
					if err != nil {
						t.Fatal(err)
					}
				}
				f.restart(t)
				path, err := f.store.SessionLogPath(ctx, "session")
				if err != nil {
					t.Fatal(err)
				}
				before, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if _, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 0); err != nil {
					t.Fatal(err)
				}
				after, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if before.Size() != after.Size() {
					t.Fatal("ordinary history queries must not trigger recovery")
				}
				for range 2 {
					if err := f.svc.ReconcileToolResults(ctx); err != nil {
						t.Fatal(err)
					}
				}
				expected := status
				if status == toolmodel.ToolInvocationRunning {
					expected = toolmodel.ToolInvocationUnknown
				} else if ended && status != toolmodel.ToolInvocationSucceeded {
					expected = toolmodel.ToolInvocationInterrupted
				}
				inv, err := f.repos.ToolInvocations.FindByTaskCall(ctx, task.ID, "z-first")
				if err != nil {
					t.Fatal(err)
				}
				if inv.Status != expected {
					t.Fatalf("status=%s want=%s", inv.Status, expected)
				}
				stream, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
				if err != nil {
					t.Fatal(err)
				}
				missing, err := f.repos.Messages.ListMissingToolResults(ctx)
				if err != nil {
					t.Fatal(err)
				}
				if expected.Terminal() {
					if len(missing) != 0 || len(stream.Messages) != 4 || stream.Messages[2].Blocks[0].CallID != "z-first" || stream.Messages[3].Blocks[0].CallID != "a-second" {
						t.Fatalf("incorrect receipt order or idempotency: %+v", stream)
					}
					stored, err := f.repos.Tasks.Get(ctx, task.ID)
					if err != nil {
						t.Fatal(err)
					}
					var bytes int64
					if !ended {
						for _, message := range stream.Messages[2:] {
							bytes += int64(len(message.Blocks[0].Text))
						}
					}
					if stored.Progress.OutputBytes != bytes {
						t.Fatalf("recovery accounting is not idempotent: got=%d want=%d", stored.Progress.OutputBytes, bytes)
					}
				} else if len(missing) != 2 || len(stream.Messages) != 2 {
					t.Fatal("active invocation or approval wait was incorrectly settled")
				}
			})
		}
	}
}

func TestRecoveryRollbackAndLiveRunningProtection(t *testing.T) {
	f := newHookFixture(t)
	ctx := context.Background()
	created, err := f.svc.SendInput(ctx, hookInput())
	if err != nil {
		t.Fatal(err)
	}
	addRecoveryCalls(t, f, created.Task)
	setRecoveryStatus(t, f, created.Task, toolmodel.ToolInvocationRunning)
	if _, _, err := f.svc.LoadContext(ctx, created.Task); err != nil {
		t.Fatal(err)
	}
	inv, err := f.repos.ToolInvocations.FindByTaskCall(ctx, created.Task.ID, "z-first")
	if err != nil || inv.Status != toolmodel.ToolInvocationRunning {
		t.Fatal(inv, err)
	}
	f.svc.tx = hookRollbackTx{f.store}
	if err := f.svc.ReconcileToolResults(ctx); !errors.Is(err, errHookRollback) {
		t.Fatal(err)
	}
	inv, err = f.repos.ToolInvocations.FindByTaskCall(ctx, created.Task.ID, "z-first")
	if err != nil || inv.Status != toolmodel.ToolInvocationRunning {
		t.Fatal("recovery rollback did not preserve invocation state", inv, err)
	}
	stream, err := f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	if err != nil || len(stream.Messages) != 2 {
		t.Fatal(stream, err)
	}
}
