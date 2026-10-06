package agentruntime

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/logging"

	"context"
	"errors"
	"sync"
	"time"
)

type RuntimeConfig struct {
	AgentID          contracts.AgentID
	Executions       *agentmodel.Executions
	MessageRecorders MessageRecorderResolver
	RunLoop          LoopFunc
	TaskBuilder      TaskBuilder
	Lifecycle        TaskLifecycle
	Queue            TaskQueue
	// Logger 仅记录已脱敏的诊断信息，不得记录 Provider payload 或凭据；nil 时禁用日志。
	Logger           *logging.Logger
	LifecycleTimeout time.Duration
}

// Runtime 是单个 Agent 的进程内执行器，活动状态不会跨进程恢复。
type Runtime struct {
	agentID          contracts.AgentID
	executions       *agentmodel.Executions
	messageRecorders MessageRecorderResolver
	runLoop          LoopFunc
	taskBuilder      TaskBuilder
	lifecycle        TaskLifecycle
	queue            TaskQueue
	logger           *logging.Logger
	lifecycleTimeout time.Duration

	mu      sync.Mutex
	active  *runtimeTask
	closed  bool
	running sync.WaitGroup
}

type runtimeTask struct {
	id     contracts.TaskID
	ending bool
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	if config.AgentID == "" {
		return nil, errors.New("target runtime agent id is required")
	}
	if config.Executions == nil {
		return nil, errors.New("agent execution state is required")
	}
	if config.MessageRecorders == nil {
		return nil, errors.New("target runtime message recorder resolver is required")
	}
	if config.RunLoop == nil {
		return nil, errors.New("target runtime loop function is required")
	}
	if config.TaskBuilder == nil {
		return nil, errors.New("target runtime task builder is required")
	}
	if config.Lifecycle == nil {
		return nil, errors.New("target runtime task lifecycle is required")
	}
	if config.Queue == nil {
		return nil, errors.New("target runtime task queue is required")
	}
	timeout := config.LifecycleTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Runtime{
		agentID:          config.AgentID,
		executions:       config.Executions,
		messageRecorders: config.MessageRecorders,
		runLoop:          config.RunLoop,
		taskBuilder:      config.TaskBuilder,
		lifecycle:        config.Lifecycle,
		queue:            config.Queue,
		logger:           logging.NewFactory().Ensure(config.Logger),
		lifecycleTimeout: timeout,
	}, nil
}

// Activate 只接受属于当前 Agent 且已持久化的 task。
// lifecycle 负责执行开始和结束的状态更新；重复激活幂等，仍在执行的另一个 task 会被拒绝。
func (r *Runtime) Activate(
	ctx context.Context,
	task taskmodel.Task,
) error {
	if ctx == nil {
		return errors.New("target runtime activation context is required")
	}
	if err := task.Validate(); err != nil {
		return err
	}
	if task.AgentID != r.agentID || !task.Active() {
		return errors.New("target runtime received an ineligible task")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activateLocked(task)
}

func (r *Runtime) activateLocked(task taskmodel.Task) error {
	if r.closed {
		return errors.New("target runtime is closed")
	}
	if r.active != nil {
		if r.active.id == task.ID {
			return nil
		}
		if !r.active.ending {
			return errors.New("target runtime already has an active task")
		}
	}
	taskCtx, err := r.executions.Context(r.agentID, task.ID)
	if err != nil {
		return err
	}
	active := &runtimeTask{id: task.ID}
	r.active = active
	r.running.Go(func() { r.run(taskCtx, task, active) })
	r.logEvent(task.ID, "activation accepted")
	return nil
}

// StartNext 从 runtime 持有的队列领取并执行下一项；正在执行时保持排队，关闭后拒绝领取。
// 领取和执行槽交接共用锁，避免并发唤醒重复领取或关闭后留下尚未执行的任务。
func (r *Runtime) StartNext(ctx context.Context) (bool, error) {
	if ctx == nil {
		return false, errors.New("runtime queue context is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return false, errors.New("target runtime is closed")
	}
	if r.active != nil {
		return false, nil
	}
	pending, found, err := r.queue.Next(ctx)
	if err != nil || !found {
		return false, err
	}
	task, prepared, err := r.taskBuilder.BuildTask(ctx, pending)
	if err != nil || !prepared {
		return false, err
	}
	if task.AgentID != r.agentID {
		return false, errors.New("runtime task belongs to another agent")
	}
	if err := r.activateLocked(task); err != nil {
		return false, err
	}
	return true, nil
}

// Close 拒绝新调度，请求 Agent 停止活动执行，并等待至调用方给定的 deadline。
func (r *Runtime) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("target runtime close context is required")
	}
	r.mu.Lock()
	r.closed = true
	active := r.active
	if active != nil {
		r.executions.Stop(r.agentID, active.id)
	}
	r.mu.Unlock()
	// 关闭必须同时等待执行、结束事务和队列交接，防止释放持久化资源后继续写入。
	done := make(chan struct{})
	go func() {
		r.running.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Runtime) run(
	ctx context.Context,
	task taskmodel.Task,
	active *runtimeTask,
) {
	r.logEvent(task.ID, "task started")
	defer r.finishTask(active)

	if err := r.startTask(task); err != nil {
		outcome, code, message := r.executionResult(ctx, taskmodel.TaskFailed, contracts.TaskFailureRuntimeFailed, err)
		r.endTask(task, active, outcome, code, message)
		return
	}
	if ctx.Err() != nil {
		outcome, code, message := r.executionResult(ctx, taskmodel.TaskInterrupted, contracts.TaskFailureRuntimeCancelled, ctx.Err())
		r.endTask(task, active, outcome, code, message)
		return
	}

	messageRecorder, err := r.messageRecorders(ctx, task.SessionID, task.AgentID)
	if err != nil || messageRecorder == nil {
		if err == nil {
			err = errors.New("message resolver returned a nil store")
		}
		r.logError(task, "resolve message recorder", err)
		outcome, code, message := r.executionResult(ctx, taskmodel.TaskFailed, contracts.TaskFailureRuntimeFailed, err)
		r.endTask(task, active, outcome, code, message)
		return
	}

	outcome, failureCode, failureMessage := r.execute(ctx, task, messageRecorder)
	if !r.endTask(task, active, outcome, failureCode, failureMessage) {
		return
	}
	r.logEvent(task.ID, "task ended")
}

// finishTask 仅释放属于本轮的活动槽位，不清除队列交接已启动的下一轮。
func (r *Runtime) finishTask(active *runtimeTask) {
	r.mu.Lock()
	r.executions.End(r.agentID, active.id)
	if r.active == active {
		r.active = nil
	}
	r.mu.Unlock()
}

func (r *Runtime) startTask(task taskmodel.Task) error {
	startContext, cancelStart := r.lifecycleContext()
	defer cancelStart()
	if err := r.lifecycle.Start(startContext, task.ID); err != nil {
		r.logError(task, "start product task", err)
		return err
	}
	return nil
}

// execute 运行 Provider/tool loop，并把取消转换为可持久化的结果。
// loop 不拥有持久化生命周期，因此 context 取消后仍会交由业务服务保存结束状态。
func (r *Runtime) execute(
	ctx context.Context,
	task taskmodel.Task,
	messageRecorder MessageRecorder,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, string) {
	outcome, failureCode, err := r.runLoop(ctx, task, messageRecorder)
	r.logEvent(task.ID, "provider completed")
	if err != nil {
		r.logError(task, "run provider", err)
	}
	return r.executionResult(ctx, outcome, failureCode, err)
}

// executionResult 读取 Agent 提供的取消原因；持久化控制命令由结束事务最终仲裁。
func (r *Runtime) executionResult(
	ctx context.Context,
	outcome taskmodel.TaskOutcome,
	failureCode contracts.TaskFailureCode,
	err error,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, string) {
	failureMessage := contracts.TaskFailureMessage(failureCode, err)
	if ctx.Err() != nil {
		outcome = taskmodel.TaskInterrupted
		if errors.Is(context.Cause(ctx), agentmodel.ErrExecutionPaused) {
			outcome = taskmodel.TaskPaused
		}
		failureCode = contracts.TaskFailureRuntimeCancelled
		failureMessage = ""
	} else if err != nil {
		outcome = taskmodel.TaskFailed
		if failureCode == "" {
			failureCode = contracts.TaskFailureRuntimeFailed
		}
	}
	outcome, failureCode = normalizeOutcome(outcome, failureCode)
	if failureCode != contracts.TaskFailureProvider {
		failureMessage = ""
	}
	return outcome, failureCode, failureMessage
}

func (r *Runtime) endTask(
	task taskmodel.Task,
	active *runtimeTask,
	outcome taskmodel.TaskOutcome,
	failureCode contracts.TaskFailureCode,
	failureMessage string,
) bool {
	// loop 已返回，允许已通过持久化准入的下一轮接管；同一 Task 的重复激活仍保持幂等。
	r.mu.Lock()
	active.ending = true
	r.executions.BeginEnding(r.agentID, active.id)
	r.mu.Unlock()
	endContext, cancelEnd := r.lifecycleContext()
	defer cancelEnd()
	endedTask, err := r.lifecycle.End(
		endContext,
		task.ID,
		outcome,
		failureCode,
		failureMessage,
	)
	if err != nil {
		r.logError(task, "end product task", err)
		return false
	}
	// 结算提交后才释放执行槽；入队唤醒不能在暂停或取消结算途中抢先续跑。
	r.finishTask(active)
	// 只有已提交的最终结果允许续跑；已提交的暂停或取消可以覆盖 loop 的正常完成。
	if endedTask.Outcome == taskmodel.TaskCompleted || endedTask.Outcome == taskmodel.TaskYielded {
		queueContext, cancelQueue := r.lifecycleContext()
		defer cancelQueue()
		if _, err := r.StartNext(queueContext); err != nil {
			r.logError(task, "prepare next queued task", err)
		}
	}
	return true
}

func normalizeOutcome(
	outcome taskmodel.TaskOutcome,
	failureCode contracts.TaskFailureCode,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode) {
	if !knownOutcome(outcome) {
		outcome = taskmodel.TaskFailed
		failureCode = contracts.TaskFailureRuntimeInvalid
	}
	if outcome == taskmodel.TaskFailed && failureCode == "" {
		failureCode = contracts.TaskFailureRuntimeFailed
	}
	if !failureCode.Valid() {
		outcome = taskmodel.TaskFailed
		failureCode = contracts.TaskFailureRuntimeFailed
	}
	return outcome, failureCode
}

func (r *Runtime) logError(task taskmodel.Task, stage string, err error) {
	if err == nil {
		return
	}
	r.logger.Errorf("task id=%s stage=%s failed: %v", task.ID, stage, err)
}

func (r *Runtime) logEvent(taskID contracts.TaskID, stage string) {
	r.logger.Infof("task id=%s stage=%s", taskID, stage)
}

func (r *Runtime) lifecycleContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.lifecycleTimeout)
}

func knownOutcome(outcome taskmodel.TaskOutcome) bool {
	switch outcome {
	case taskmodel.TaskCompleted, taskmodel.TaskYielded, taskmodel.TaskPaused,
		taskmodel.TaskFailed, taskmodel.TaskInterrupted:
		return true
	default:
		return false
	}
}
