// Package execution 协调进程内 Execution 执行、审批等待和持久化队列交接。
package coordinator

import (
	"context"
	"errors"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/logging"
	agentmodel "praxis/internal/service/agent"
	"sync"
	"time"
)

type Config struct {
	ConfigMu          *sync.RWMutex
	Recovery          RecoverySource
	Approvals         ToolApprovals
	Loop              *loop.Runner
	ActiveTasks       *ActiveTasks
	Operations        ExecutionOperations
	Queue             TaskQueue
	Messages          toolexecution.WriterProvider
	HookDispatcher    *hook.Dispatcher
	Logger            *logging.Logger
	TransitionTimeout time.Duration
}

// Coordinator 是进程级执行协调器；活动身份只保存在共享的 ActiveTasks 中。
type Coordinator struct {
	configMu          *sync.RWMutex
	recovery          RecoverySource
	approvals         ToolApprovals
	loop              *loop.Runner
	activeTasks       *ActiveTasks
	operations        ExecutionOperations
	queue             TaskQueue
	messages          toolexecution.WriterProvider
	hookDispatcher    *hook.Dispatcher
	logger            *logging.Logger
	transitionTimeout time.Duration

	// 此锁仅协调关闭与后台工作的启动；存储和模型调用必须在锁外执行。
	mu      sync.Mutex
	closed  bool
	running sync.WaitGroup
}

func NewCoordinator(config Config) (*Coordinator, error) {
	if config.Loop == nil || config.ActiveTasks == nil || config.Operations == nil || config.Queue == nil || config.Messages == nil {
		return nil, errors.New("execution loop, active tasks, task operations, queue and message streams are required")
	}
	timeout := config.TransitionTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if config.ConfigMu == nil {
		config.ConfigMu = &sync.RWMutex{}
	}
	return &Coordinator{
		configMu:          config.ConfigMu,
		recovery:          config.Recovery,
		approvals:         config.Approvals,
		loop:              config.Loop,
		activeTasks:       config.ActiveTasks,
		operations:        config.Operations,
		queue:             config.Queue,
		messages:          config.Messages,
		hookDispatcher:    config.HookDispatcher,
		logger:            logging.NewFactory().Ensure(config.Logger),
		transitionTimeout: timeout,
	}, nil
}

// enter 将前台恢复、队列准备也计入关闭等待，禁止关闭 Store 后继续写入。
func (r *Coordinator) enter(ctx context.Context) error {
	if ctx == nil {
		return errors.New("execution context is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("execution coordinator is closed")
	}
	r.running.Add(1)
	return nil
}

// Activate 接管已持久化的活动 Execution，重复调用只领取一次运行权。
func (r *Coordinator) Activate(ctx context.Context, task agentmodel.Execution) error {
	if err := r.enter(ctx); err != nil {
		return err
	}
	defer r.running.Done()
	if !task.Active() {
		return nil
	}
	return r.activate(ctx, task)
}

func (r *Coordinator) activate(ctx context.Context, task agentmodel.Execution) error {
	if _, err := r.activeTasks.Context(task.AgentID, task.ID); errors.Is(err, fault.ErrNotFound) {
		reclaimed, err := r.operations.ReclaimExecution(ctx, task)
		if err != nil {
			return err
		}
		task = reclaimed
	} else if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		// 恢复事务可能在 Close 发送取消后才预留身份，补发取消但保留持久化恢复能力。
		r.activeTasks.Stop(task.AgentID, task.ID)
		return errors.New("execution coordinator is closed")
	}
	active, claimed, err := r.activeTasks.TryRun(task.AgentID, task.ID)
	if err != nil || !claimed {
		return err
	}
	r.running.Go(func() { r.run(active.ctx, task, active) })
	return nil
}

// ResumeApproved 在审批事务提交后唤醒同一执行；尚未建立协程时正常激活。
func (r *Coordinator) ResumeApproved(ctx context.Context, task agentmodel.Execution) error {
	if err := r.Activate(ctx, task); err != nil {
		return err
	}
	r.activeTasks.Wake(task.AgentID, task.ID)
	return nil
}

// Close 拒绝新任务，取消所有运行，并等待准备、恢复和结算操作退出。
// 超时后资源仍由 Application 保留，后续 Close 可以继续等待。
func (r *Coordinator) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("execution close context is required")
	}
	r.mu.Lock()
	r.closed = true
	r.activeTasks.stopAll()
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { r.running.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Coordinator) transitionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.transitionTimeout)
}
