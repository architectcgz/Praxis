package task

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	taskmodel "praxis/internal/core/task"
)

func TestDistinctRequestsCompeteForOneAgent(t *testing.T) {
	f := newHookFixture(t)
	var created atomic.Int32
	var wg sync.WaitGroup
	for i := range 8 {
		wg.Go(func() {
			params := hookInput()
			params.RequestID = contracts.RequestID(fmt.Sprint("request-", i))
			if _, err := f.svc.SendInput(context.Background(), params); err == nil {
				created.Add(1)
			}
		})
	}
	wg.Wait()
	if created.Load() != 1 {
		t.Fatalf("same agent started %d tasks", created.Load())
	}
}

func TestSessionDeletionCompetesWithInput(t *testing.T) {
	f := newHookFixture(t)
	ctx := context.Background()
	start := make(chan struct{})
	var inputErr, deleteErr error
	var wg sync.WaitGroup
	wg.Go(func() { <-start; _, inputErr = f.svc.SendInput(ctx, hookInput()) })
	wg.Go(func() {
		<-start
		deleteErr = f.store.InTx(ctx, func(txCtx context.Context) error { _, err := f.repos.Sessions.Delete(txCtx, "session"); return err })
	})
	close(start)
	wg.Wait()
	if (inputErr == nil) == (deleteErr == nil) {
		t.Fatalf("deletion and startup must share a transaction boundary: input=%v delete=%v", inputErr, deleteErr)
	}
}

func TestCancelCompetesWithSettlement(t *testing.T) {
	f := newHookFixture(t)
	ctx := context.Background()
	created, err := f.svc.SendInput(ctx, hookInput())
	if err != nil {
		t.Fatal(err)
	}
	start := make(chan struct{})
	var commandErr, settleErr error
	var ended taskmodel.Task
	var wg sync.WaitGroup
	wg.Go(func() {
		<-start
		commandErr = f.store.InTx(ctx, func(txCtx context.Context) error {
			task, err := f.repos.Tasks.Get(txCtx, created.Task.ID)
			if err != nil {
				return err
			}
			if !task.Active() {
				return contracts.ErrRequestConflict
			}
			command, err := agentmodel.NewAgentControlCommand("cancel", "agent", task.ID, agentmodel.AgentControlCancel, time.Now())
			if err != nil {
				return err
			}
			return f.repos.Controls.Save(txCtx, command)
		})
	})
	wg.Go(func() {
		<-start
		ended, settleErr = f.svc.Settle(ctx, created.Task.ID, taskmodel.TaskCompleted, "", "")
	})
	close(start)
	wg.Wait()
	if settleErr != nil {
		t.Fatal(settleErr)
	}
	if commandErr == nil && ended.Outcome != taskmodel.TaskInterrupted || commandErr != nil && ended.Outcome != taskmodel.TaskCompleted {
		t.Fatalf("commit order did not determine the final outcome: command=%v outcome=%s", commandErr, ended.Outcome)
	}
}
