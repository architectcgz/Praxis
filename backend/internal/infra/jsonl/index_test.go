package jsonl

import (
	"context"
	"errors"
	"testing"
	"time"

	"praxis/internal/contracts"
	taskmodel "praxis/internal/core/task"
)

func TestPendingIndexSeesFinalTransactionState(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	for _, id := range []contracts.TaskID{"one", "two"} {
		requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error {
			sequence, err := f.repos.Tasks.NextSequence(txCtx, "agent")
			if err != nil {
				return err
			}
			task, err := taskmodel.NewPendingTask(id, "session", "agent", contracts.RequestID(id), "input:"+id.String(), sequence, time.Now())
			if err != nil {
				return err
			}
			return f.repos.Tasks.Save(txCtx, task)
		}))
	}
	requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error {
		first, err := f.repos.Tasks.FindNextPendingByAgent(txCtx, "agent")
		if err != nil {
			return err
		}
		if first.ID != "one" {
			t.Fatal(first)
		}
		if err := first.Prepare(f.config(), taskmodel.ContextSelection{}); err != nil {
			return err
		}
		if err := f.repos.Tasks.Save(txCtx, first); err != nil {
			return err
		}
		second, err := f.repos.Tasks.FindNextPendingByAgent(txCtx, "agent")
		if err != nil {
			return err
		}
		if second.ID != "two" {
			t.Fatal("transaction still treats a claimed task as pending")
		}
		agent := f.agent
		if err := agent.Start(first.ID, time.Now()); err != nil {
			return err
		}
		return f.repos.Agents.Save(txCtx, agent)
	}))
	f.restart(t)
	active, err := f.repos.Tasks.GetActiveByAgent(ctx, "agent")
	requireOK(t, err)
	if active.ID != "one" {
		t.Fatal(active)
	}
	pending, err := f.repos.Tasks.FindNextPendingByAgent(ctx, "agent")
	requireOK(t, err)
	if pending.ID != "two" {
		t.Fatal(pending)
	}
	seq, err := f.repos.Tasks.NextSequence(ctx, "agent")
	requireOK(t, err)
	if seq != 3 {
		t.Fatal(seq)
	}
}

func TestSessionDeletionClearsIndexesAndCannotResurrect(t *testing.T) {
	f := newStoreFixture(t)
	ctx := context.Background()
	_, err := f.repos.Messages.Append(ctx, f.agent, userMessage("message"))
	requireOK(t, err)
	_, err = f.repos.Messages.LoadMessages(ctx, "session", "agent", 0)
	requireOK(t, err)
	requireOK(t, f.store.InTx(ctx, func(txCtx context.Context) error { _, err := f.repos.Sessions.Delete(txCtx, "session"); return err }))
	if len(f.store.index.groups[scopeKey("session")]) != 0 || f.store.index.hasMessages["session"] || len(f.store.messages.sessions) != 0 {
		t.Fatal("ownership index remains after deletion")
	}
	for range 2 {
		if _, err := f.repos.Sessions.Get(ctx, "session"); !errors.Is(err, contracts.ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := f.repos.Agents.Get(ctx, "agent"); !errors.Is(err, contracts.ErrNotFound) {
			t.Fatal(err)
		}
		if _, err := f.repos.Messages.Append(ctx, f.agent, userMessage("late")); err == nil {
			t.Fatal("message writes after deletion must be rejected")
		}
		f.restart(t)
	}
}
