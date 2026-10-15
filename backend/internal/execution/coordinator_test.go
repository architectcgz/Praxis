package execution

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"praxis/internal/contracts"
	contextmodel "praxis/internal/core/context"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/loop"
	"praxis/internal/tools"
	toolcontracts "praxis/internal/tools/contracts"
)

// fixture 使用真实 Loop；替身只控制存储、模型和工具边界的提交时机。
type fixture struct {
	mu          sync.Mutex
	slots       *ActiveTasks
	tasks       map[contracts.TaskID]taskmodel.Task
	pending     map[contracts.AgentID]contracts.TaskID
	builds      map[contracts.TaskID]int
	settled     chan taskmodel.Task
	build       func(context.Context, taskmodel.Task) (contextmodel.BuildResult, error)
	settle      func(context.Context, taskmodel.Task) error
	arbitrate   func(taskmodel.Task) taskmodel.Task
	prepare     func(context.Context) error
	tool        func(context.Context) (toolcontracts.ToolExecutionResult, error)
	coordinator *Coordinator
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	f := &fixture{
		slots:   &ActiveTasks{},
		tasks:   make(map[contracts.TaskID]taskmodel.Task),
		pending: make(map[contracts.AgentID]contracts.TaskID),
		builds:  make(map[contracts.TaskID]int),
		settled: make(chan taskmodel.Task, 20),
	}
	runner, err := loop.NewRunner(loop.Config{
		ContextBuilder: f,
		Progress:       f,
		ModelBuilder:   completeModel{},
		Tools:          tools.NewToolRegistry(),
		ToolExecutor:   f,
		EventObserver:  func(loop.Event) {},
	})
	if err != nil {
		t.Fatal(err)
	}
	f.coordinator, err = NewCoordinator(Config{
		Loop:        &runner,
		ActiveTasks: f.slots,
		Tasks:       f,
		Queue:       f,
		MessageRecorders: func(context.Context, contracts.SessionID, contracts.AgentID) (loop.MessageRecorder, error) {
			return memoryRecorder{}, nil
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := f.coordinator.Close(ctx); err != nil {
			t.Error(err)
		}
	})
	return f
}

func (f *fixture) add(t *testing.T, id contracts.TaskID, agentID contracts.AgentID, reserve bool) taskmodel.Task {
	t.Helper()
	task, err := taskmodel.NewTask(id, "session-"+contracts.SessionID(agentID), agentID, "request-"+contracts.RequestID(id), taskmodel.Input{
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
	f.mu.Lock()
	f.tasks[id] = task
	f.mu.Unlock()
	if reserve {
		if _, _, err := f.slots.Reserve(t.Context(), agentID, id); err != nil {
			t.Fatal(err)
		}
	}
	return task
}

func (f *fixture) ReclaimTask(ctx context.Context, task taskmodel.Task) (taskmodel.Task, error) {
	_, _, err := f.slots.Reserve(ctx, task.AgentID, task.ID)
	return task, err
}
func (f *fixture) ConfirmRunning(context.Context, contracts.TaskID) error { return nil }
func (f *fixture) Settle(ctx context.Context, id contracts.TaskID, outcome taskmodel.TaskOutcome, code contracts.TaskFailureCode, message string) (taskmodel.Task, error) {
	f.mu.Lock()
	task := f.tasks[id]
	task.Status, task.Outcome, task.FailureCode, task.FailureMessage = taskmodel.TaskEnded, outcome, code, message
	callback := f.settle
	arbitrate := f.arbitrate
	f.mu.Unlock()
	if arbitrate != nil {
		task = arbitrate(task)
	}
	if callback != nil {
		if err := callback(ctx, task); err != nil {
			return taskmodel.Task{}, err
		}
	}
	f.mu.Lock()
	f.tasks[id] = task
	f.mu.Unlock()
	f.settled <- task
	return task, nil
}
func (f *fixture) Next(_ context.Context, agentID contracts.AgentID) (taskmodel.Task, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	id, found := f.pending[agentID]
	return f.tasks[id], found, nil
}
func (f *fixture) PrepareQueuedTask(ctx context.Context, selected taskmodel.Task) (taskmodel.Task, bool, error) {
	if f.prepare != nil {
		if err := f.prepare(ctx); err != nil {
			return taskmodel.Task{}, false, err
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.pending[selected.AgentID] != selected.ID {
		return taskmodel.Task{}, false, nil
	}
	if _, _, err := f.slots.Reserve(ctx, selected.AgentID, selected.ID); err != nil {
		return taskmodel.Task{}, false, err
	}
	delete(f.pending, selected.AgentID)
	return selected, true, nil
}
func (f *fixture) LoadTask(_ context.Context, id contracts.TaskID) (taskmodel.Task, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.tasks[id], nil
}
func (f *fixture) RecordModelRequest(context.Context, contracts.TaskID, uint64) error { return nil }
func (f *fixture) RecordModelOutput(context.Context, contracts.TaskID, int) error     { return nil }
func (f *fixture) RebuildTaskContext(ctx context.Context, task taskmodel.Task) (contextmodel.BuildResult, error) {
	f.mu.Lock()
	f.builds[task.ID]++
	callback := f.build
	f.mu.Unlock()
	if callback != nil {
		return callback(ctx, task)
	}
	return contextmodel.BuildResult{}, nil
}
func (f *fixture) Execute(ctx context.Context, _ contracts.ToolCall, _ contracts.ToolInvocationContext) (toolcontracts.ToolExecutionResult, error) {
	if f.tool != nil {
		return f.tool(ctx)
	}
	return toolcontracts.ToolExecutionResult{}, nil
}

type completeModel struct{}

func (completeModel) BuildModel(model.Config) (model.Model, error) {
	return model.Model{Stream: completeStream{}, MaxOutputTokens: 1024}, nil
}

type completeStream struct{}

func (completeStream) Stream(context.Context, model.ModelRequest) (<-chan model.ModelStreamEvent, error) {
	events := make(chan model.ModelStreamEvent, 1)
	events <- model.ModelStreamEvent{Kind: model.StreamComplete, StopReason: model.StopCompleted}
	close(events)
	return events, nil
}

type memoryRecorder struct{}

func (memoryRecorder) Append(_ context.Context, message sessionmodel.MessageData) (sessionmodel.MessageData, error) {
	return message, nil
}
func (memoryRecorder) AppendAssistant(context.Context, sessionmodel.MessageData, contracts.ToolInvocationContext) error {
	return nil
}

func receive[T any](t *testing.T, values <-chan T) T {
	t.Helper()
	select {
	case value := <-values:
		return value
	case <-time.After(3 * time.Second):
		t.Fatal("等待执行事件超时")
		var zero T
		return zero
	}
}
func waitIdle(t *testing.T, f *fixture, task taskmodel.Task) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		f.slots.mu.Lock()
		active := f.slots.active[task.AgentID]
		idle := active == nil || !active.running
		f.slots.mu.Unlock()
		if idle {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("执行没有退出")
}

func TestDuplicateActivationAndParallelAgents(t *testing.T) {
	f := newFixture(t)
	first := f.add(t, "first", "a", true)
	second := f.add(t, "second", "b", true)
	entered := make(chan contracts.TaskID, 2)
	release := make(chan struct{})
	f.build = func(ctx context.Context, task taskmodel.Task) (contextmodel.BuildResult, error) {
		entered <- task.ID
		select {
		case <-release:
		case <-ctx.Done():
			return contextmodel.BuildResult{}, ctx.Err()
		}
		return contextmodel.BuildResult{}, nil
	}
	if err := f.coordinator.Activate(t.Context(), first); err != nil {
		t.Fatal(err)
	}
	receive(t, entered)
	var calls sync.WaitGroup
	for range 30 {
		calls.Go(func() {
			if err := f.coordinator.Activate(t.Context(), first); err != nil {
				t.Error(err)
			}
		})
	}
	calls.Wait()
	if err := f.coordinator.Activate(t.Context(), second); err != nil {
		t.Fatal(err)
	}
	receive(t, entered) // 第二个 Agent 必须能在第一个 Agent 尚未结束时进入 Loop。
	close(release)
	receive(t, f.settled)
	receive(t, f.settled)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.builds[first.ID] != 1 || f.builds[second.ID] != 1 {
		t.Fatalf("重复运行: %v", f.builds)
	}
}

func TestApprovalBeforeWaitIsNotLost(t *testing.T) {
	f := newFixture(t)
	task := f.add(t, "task", "a", true)
	f.build = func(context.Context, taskmodel.Task) (contextmodel.BuildResult, error) {
		return contextmodel.BuildResult{TaskMessages: []sessionmodel.MessageData{{
			Role:   sessionmodel.RoleAssistant,
			TurnID: "turn",
			Blocks: []sessionmodel.Block{{Kind: sessionmodel.BlockToolCall, CallID: "call", Name: "bash", Input: []byte(`{}`)}},
		}}}, nil
	}
	entered := make(chan struct{})
	release := make(chan struct{})
	var attempts atomic.Int32
	f.tool = func(context.Context) (toolcontracts.ToolExecutionResult, error) {
		if attempts.Add(1) == 1 {
			close(entered)
			<-release
			return toolcontracts.ToolExecutionResult{ApprovalPending: true}, nil
		}
		return toolcontracts.ToolExecutionResult{}, nil
	}
	if err := f.coordinator.Activate(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	receive(t, entered)
	// 模拟审批已提交，但 Loop 仍未返回 awaiting_approval。
	if err := f.coordinator.ResumeApproved(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	close(release)
	ended := receive(t, f.settled)
	if ended.Outcome != taskmodel.TaskCompleted || attempts.Load() != 2 {
		t.Fatalf("审批恢复失败: %+v, attempts=%d", ended, attempts.Load())
	}
}

func TestSettlementFailureRetainsSlotAndRetriesOnlySettlement(t *testing.T) {
	f := newFixture(t)
	task := f.add(t, "task", "a", true)
	failed := make(chan struct{})
	var attempts atomic.Int32
	f.settle = func(context.Context, taskmodel.Task) error {
		if attempts.Add(1) == 1 {
			close(failed)
			return errors.New("storage unavailable")
		}
		return nil
	}
	if err := f.coordinator.Activate(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	receive(t, failed)
	waitIdle(t, f, task)
	if _, _, err := f.slots.Reserve(t.Context(), task.AgentID, "other"); !errors.Is(err, contracts.ErrAgentExecuting) {
		t.Fatalf("结算失败释放了占用: %v", err)
	}
	if started, err := f.coordinator.StartNext(t.Context(), task.AgentID); err != nil || started {
		t.Fatalf("失败后继续领取: %v %v", started, err)
	}
	if err := f.coordinator.Activate(t.Context(), task); err != nil {
		t.Fatal(err)
	}
	receive(t, f.settled)
	waitIdle(t, f, task)
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.builds[task.ID] != 1 || attempts.Load() != 2 {
		t.Fatalf("重跑了 Loop: builds=%v attempts=%d", f.builds, attempts.Load())
	}
}

func TestQueueWaitsForCommittedSettlement(t *testing.T) {
	for _, final := range []taskmodel.TaskOutcome{taskmodel.TaskCompleted, taskmodel.TaskPaused, taskmodel.TaskInterrupted} {
		t.Run(string(final), func(t *testing.T) {
			f := newFixture(t)
			first := f.add(t, "first", "a", true)
			second := f.add(t, "second", "a", false)
			f.pending[first.AgentID] = second.ID
			entered := make(chan struct{})
			release := make(chan struct{})
			f.settle = func(_ context.Context, task taskmodel.Task) error {
				if task.ID == first.ID {
					close(entered)
					<-release
				}
				return nil
			}
			// Loop 正常完成，但存储事务中的控制意图覆盖其结果。
			f.arbitrate = func(task taskmodel.Task) taskmodel.Task {
				if task.ID == first.ID {
					task.Outcome = final
				}
				return task
			}
			if err := f.coordinator.Activate(t.Context(), first); err != nil {
				t.Fatal(err)
			}
			receive(t, entered)
			if started, err := f.coordinator.StartNext(t.Context(), first.AgentID); err != nil || started {
				t.Fatalf("结算前启动队列: %v %v", started, err)
			}
			close(release)
			ended := receive(t, f.settled)
			if ended.Outcome != final {
				t.Fatalf("取消原因丢失: %+v", ended)
			}
			if final == taskmodel.TaskCompleted {
				if next := receive(t, f.settled); next.ID != second.ID {
					t.Fatalf("队列没有交接: %+v", next)
				}
			} else {
				waitIdle(t, f, first)
				f.mu.Lock()
				defer f.mu.Unlock()
				if f.builds[second.ID] != 0 {
					t.Fatal("暂停或取消后仍启动队列")
				}
			}
		})
	}
}

func TestQueuePreparationFailureDoesNotConsumePending(t *testing.T) {
	f := newFixture(t)
	task := f.add(t, "task", "a", false)
	f.pending[task.AgentID] = task.ID
	f.prepare = func(context.Context) error { return errors.New("cannot build input") }
	if started, err := f.coordinator.StartNext(t.Context(), task.AgentID); err == nil || started {
		t.Fatalf("准备失败未返回错误: %v %v", started, err)
	}
	if f.pending[task.AgentID] != task.ID || f.slots.occupied(task.AgentID) {
		t.Fatal("准备失败消费了队列或占用执行槽")
	}
}

func TestCloseWaitsForPreparationAndRejectsNewDispatch(t *testing.T) {
	f := newFixture(t)
	task := f.add(t, "task", "a", false)
	f.pending[task.AgentID] = task.ID
	entered := make(chan struct{})
	release := make(chan struct{})
	f.prepare = func(context.Context) error { close(entered); <-release; return nil }
	dispatch := make(chan error, 1)
	go func() { _, err := f.coordinator.StartNext(t.Context(), task.AgentID); dispatch <- err }()
	receive(t, entered)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
	defer cancel()
	if err := f.coordinator.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("关闭没有等待持久化: %v", err)
	}
	if _, err := f.coordinator.StartNext(t.Context(), task.AgentID); err == nil {
		t.Fatal("关闭后继续派发")
	}
	close(release)
	if err := receive(t, dispatch); err == nil {
		t.Fatal("关闭后准备完成的 Task 不应启动 Loop")
	}
	if err := f.coordinator.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.builds[task.ID] != 0 {
		t.Fatal("关闭期间启动了 Loop")
	}
}

type recoveryTasks struct {
	tasks  []taskmodel.Task
	agents []contracts.AgentID
}

func (s recoveryTasks) ListActive(context.Context) ([]taskmodel.Task, error) { return s.tasks, nil }
func (s recoveryTasks) ListDispatchableAgents(context.Context) ([]contracts.AgentID, error) {
	return s.agents, nil
}

func TestRestoreActiveTaskAndPendingQueue(t *testing.T) {
	f := newFixture(t)
	active := f.add(t, "active", "a", false)
	pending := f.add(t, "pending", "b", false)
	f.pending[pending.AgentID] = pending.ID
	if err := f.coordinator.Restore(t.Context(), recoveryTasks{
		tasks:  []taskmodel.Task{active},
		agents: []contracts.AgentID{active.AgentID, pending.AgentID},
	}); err != nil {
		t.Fatal(err)
	}
	receive(t, f.settled)
	receive(t, f.settled)
}
