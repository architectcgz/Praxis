package compose

import (
	"context"
	"errors"
	"testing"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/core/model"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/execution"
	"praxis/internal/infra/jsonl"
	"praxis/internal/loop"
	applicationsession "praxis/internal/service/session"
)

type closingTasks struct {
	execution.TaskOperations
	store   *jsonl.Store
	entered chan struct{}
	release chan struct{}
	settled chan error
}

func (s closingTasks) ConfirmRunning(context.Context, contracts.TaskID) error {
	close(s.entered)
	<-s.release
	return nil
}
func (s closingTasks) Settle(ctx context.Context, _ contracts.TaskID, outcome taskmodel.TaskOutcome, _ contracts.TaskFailureCode, _ string) (taskmodel.Task, error) {
	err := s.store.InTx(ctx, func(context.Context) error { return nil })
	s.settled <- err
	return taskmodel.Task{Outcome: outcome}, err
}

func TestApplicationCloseTimeoutKeepsStoreAvailableUntilExecutionExits(t *testing.T) {
	store, err := jsonl.Open(t.Context(), t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(context.Background()) })
	tasks := closingTasks{
		store:   store,
		entered: make(chan struct{}),
		release: make(chan struct{}),
		settled: make(chan error, 1),
	}
	slots := &execution.ActiveTasks{}
	task, err := taskmodel.NewTask("task", "session", "agent", "request", taskmodel.Input{
		AgentDefinitionID:       "primary",
		AgentDefinitionRevision: "revision",
		CurrentInputMessageID:   "input",
		WorkspacePath:           t.TempDir(),
		Model: model.Config{
			ProviderID:      "provider",
			ModelID:         "model",
			APIFormat:       "openai_chat",
			BaseURL:         "https://example.com",
			ContextWindow:   8192,
			MaxOutputTokens: 1024,
		},
	}, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := slots.Reserve(t.Context(), task.AgentID, task.ID); err != nil {
		t.Fatal(err)
	}
	coordinator, err := execution.NewCoordinator(execution.Config{
		Loop:        &loop.Runner{},
		ActiveTasks: slots,
		Tasks:       tasks,
		Queue:       storeQueue{},
		MessageRecorders: func(context.Context, contracts.SessionID, contracts.AgentID) (loop.MessageRecorder, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	app := &Application{
		coordinator: coordinator,
		store:       store,
		sessions:    &applicationsession.Service{},
	}
	if err := coordinator.Activate(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	select {
	case <-tasks.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("执行未开始")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := app.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("关闭结果: %v", err)
	}
	if err := store.InTx(t.Context(), func(context.Context) error { return nil }); err != nil {
		t.Fatalf("执行未退出就关闭了存储: %v", err)
	}
	close(tasks.release)
	select {
	case err := <-tasks.settled:
		if err != nil {
			t.Fatalf("后台结算无法访问存储: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("结算未完成")
	}
	if err := app.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := store.InTx(t.Context(), func(context.Context) error { return nil }); err == nil {
		t.Fatal("执行退出后未关闭存储")
	}
}

type storeQueue struct{}

func (storeQueue) Next(context.Context, contracts.AgentID) (taskmodel.Task, bool, error) {
	return taskmodel.Task{}, false, nil
}
