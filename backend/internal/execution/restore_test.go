package execution

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/loop"
	"praxis/internal/repository"
	toolcontracts "praxis/internal/tools/contracts"
)

type restoreOperations struct {
	mu                     sync.Mutex
	task                   taskmodel.Task
	active                 *ActiveTasks
	prepared               bool
	starts, loads, settles int
	failSettlement         bool
	started                chan struct{}
	allow                  chan struct{}
}

func (s *restoreOperations) ReclaimTask(ctx context.Context, task taskmodel.Task) (taskmodel.Task, error) {
	_, _, err := s.active.Reserve(ctx, task.AgentID, task.ID)
	return task, err
}
func (s *restoreOperations) ConfirmRunning(context.Context, contracts.TaskID) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts++
	return nil
}
func (s *restoreOperations) LoadContext(context.Context, taskmodel.Task) (appcontext.Content, []sessionmodel.MessageData, error) {
	s.mu.Lock()
	s.loads++
	if s.loads == 1 {
		close(s.started)
	}
	s.mu.Unlock()
	<-s.allow
	return appcontext.Content{Entries: []appcontext.ContextEntry{{Kind: appcontext.ContextEntryUserInput, Role: appcontext.ContextRoleUser, Content: []appcontext.ContextBlock{{Kind: appcontext.ContextBlockText, Text: "问题"}}}}}, nil, nil
}
func (s *restoreOperations) Settle(_ context.Context, _ contracts.TaskID, outcome taskmodel.TaskOutcome, _ contracts.TaskFailureCode, _ string) (taskmodel.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.settles++
	if s.failSettlement {
		s.failSettlement = false
		return taskmodel.Task{}, errors.New("settlement failed")
	}
	task := s.task
	task.Outcome = outcome
	task.Status = taskmodel.TaskEnded
	return task, nil
}
func (s *restoreOperations) PrepareQueuedTask(ctx context.Context, _ taskmodel.Task) (taskmodel.Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prepared {
		return taskmodel.Task{}, false, nil
	}
	s.prepared = true
	_, _, err := s.active.Reserve(ctx, s.task.AgentID, s.task.ID)
	return s.task, true, err
}
func (s *restoreOperations) Next(context.Context, contracts.AgentID) (taskmodel.Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.prepared {
		return taskmodel.Task{}, false, nil
	}
	return taskmodel.Task{ID: s.task.ID, AgentID: s.task.AgentID, Status: taskmodel.TaskPending}, true, nil
}
func (s *restoreOperations) LoadTask(context.Context, contracts.TaskID) (taskmodel.Task, error) {
	return s.task, nil
}
func (*restoreOperations) RecordModelRequest(context.Context, contracts.TaskID, uint64) error {
	return nil
}
func (*restoreOperations) RecordModelOutput(context.Context, contracts.TaskID, int) error { return nil }

type restoreMessages struct{ repository.MessageStreams }

func (restoreMessages) Writer(context.Context, contracts.SessionID, contracts.AgentID) (repository.MessageWriter, error) {
	return restoreWriter{}, nil
}

type restoreWriter struct{ repository.MessageWriter }
type restoreCatalog struct{ toolcontracts.ToolCatalog }

func (restoreCatalog) List() []contracts.ToolDefinition { return nil }

type restoreTools struct{ loop.ToolExecutor }
type restoreBuilder struct{}

func (restoreBuilder) BuildModel(model.Config) (model.Model, error) {
	return model.Model{}, errors.New("unexpected model retry")
}

type restoreSource struct {
	tasks  []taskmodel.Task
	agents []contracts.AgentID
}

func (s restoreSource) ListActive(context.Context) ([]taskmodel.Task, error) { return s.tasks, nil }
func (s restoreSource) ListDispatchableAgents(context.Context) ([]contracts.AgentID, error) {
	return s.agents, nil
}

func newRestoreCoordinator(t *testing.T) (*Coordinator, *restoreOperations) {
	t.Helper()
	config := taskmodel.Config{
		AgentDefinitionID: "primary", WorkspacePath: t.TempDir(),
		Model: model.Config{
			ProviderID:      "provider",
			ModelID:         "model",
			APIFormat:       "openai_chat",
			BaseURL:         "https://example.com",
			ContextWindow:   8192,
			MaxOutputTokens: 1024,
		},
	}
	task, err := taskmodel.NewTask("task", "session", "agent", "request", "input", config, taskmodel.ContextSelection{}, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	task.Progress.FinalAssistantID = "final"
	task.Progress.FinalStopReason = "completed"
	ops := &restoreOperations{
		task:    task,
		active:  &ActiveTasks{},
		started: make(chan struct{}),
		allow:   make(chan struct{}),
	}
	runner, err := loop.NewRunner(loop.Config{
		Progress:      ops,
		ModelBuilder:  restoreBuilder{},
		Tools:         restoreCatalog{},
		ToolExecutor:  restoreTools{},
		EventObserver: func(loop.Event) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	coordinator, err := NewCoordinator(Config{
		Loop:        &runner,
		ActiveTasks: ops.active,
		Tasks:       ops,
		Queue:       ops,
		Messages:    restoreMessages{},
	})
	if err != nil {
		t.Fatal(err)
	}
	// 非队列测试中 Next 默认为空。
	ops.prepared = true
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = coordinator.Close(ctx)
	})
	return coordinator, ops
}
func waitRestore(t *testing.T, r *Coordinator) {
	t.Helper()
	done := make(chan struct{})
	go func() { r.running.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("execution did not exit")
	}
}

func TestRestoreAndRepeatedActivationRunOnce(t *testing.T) {
	r, ops := newRestoreCoordinator(t)
	ctx := context.Background()
	if err := r.Restore(ctx, restoreSource{tasks: []taskmodel.Task{ops.task}}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ops.started:
	case <-time.After(time.Second):
		t.Fatal("recovery did not start")
	}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if err := r.Activate(ctx, ops.task); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	close(ops.allow)
	waitRestore(t, r)
	if ops.starts != 1 || ops.loads != 1 || ops.settles != 1 {
		t.Fatal(ops.starts, ops.loads, ops.settles)
	}
}

func TestRestoreDispatchesPendingQueue(t *testing.T) {
	r, ops := newRestoreCoordinator(t)
	ops.prepared = false
	close(ops.allow)
	if err := r.Restore(context.Background(), restoreSource{agents: []contracts.AgentID{"agent"}}); err != nil {
		t.Fatal(err)
	}
	waitRestore(t, r)
	if !ops.prepared || ops.starts != 1 || ops.settles != 1 {
		t.Fatal("pending recovery was not dispatched")
	}
}

func TestSettlementRetryDoesNotReenterLoop(t *testing.T) {
	r, ops := newRestoreCoordinator(t)
	ops.failSettlement = true
	close(ops.allow)
	ctx := context.Background()
	if err := r.Activate(ctx, ops.task); err != nil {
		t.Fatal(err)
	}
	waitRestore(t, r)
	if !ops.active.occupied(ops.task.AgentID) {
		t.Fatal("settlement failure must retain the reservation")
	}
	if err := r.Activate(ctx, ops.task); err != nil {
		t.Fatal(err)
	}
	waitRestore(t, r)
	if ops.starts != 1 || ops.loads != 1 || ops.settles != 2 || ops.active.occupied(ops.task.AgentID) {
		t.Fatal("settlement retry reran the loop or failed to release the reservation")
	}
}
