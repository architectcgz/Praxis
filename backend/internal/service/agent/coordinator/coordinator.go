// Package coordinator 协调进程内 Execution 执行、审批等待和内存输入队列交接。
package coordinator

import (
	"context"
	"errors"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	taskmodel "praxis/internal/core/session"
	"praxis/internal/fault"
	"praxis/internal/logging"
	"praxis/internal/repository"
	toolservice "praxis/internal/service/toolexecution"
	"sync"
	"time"
)

type Config struct {
	ConfigMu          *sync.RWMutex
	Recovery          repository.AgentExecution
	Tools             *toolservice.Service
	Sessions          repository.Session
	Workspaces        repository.Workspace
	PrimaryAgent      PrimaryAgentProvider
	SessionModels     SessionModels
	Loop              *loop.Runner
	ActiveTasks       *taskmodel.ActiveTasks
	Operations        ExecutionOperations
	SessionTasks      SessionTasks
	Events            *EventPublisher
	HookDispatcher    *hook.Dispatcher
	Logger            *logging.Logger
	TransitionTimeout time.Duration
}

// Coordinator 是进程级执行协调器；活动身份只保存在共享的 ActiveTasks 中。
type Coordinator struct {
	configMu          *sync.RWMutex
	recovery          repository.AgentExecution
	tools             *toolservice.Service
	sessions          repository.Session
	workspaces        repository.Workspace
	primary           PrimaryAgentProvider
	sessionModels     SessionModels
	restored          bool
	restoring         bool
	loop              *loop.Runner
	activeTasks       *taskmodel.ActiveTasks
	operations        ExecutionOperations
	sessionTasks      SessionTasks
	events            *EventPublisher
	hookDispatcher    *hook.Dispatcher
	logger            *logging.Logger
	transitionTimeout time.Duration

	// 此锁仅协调关闭与后台工作的启动；存储和模型调用必须在锁外执行。
	mu      sync.Mutex
	closed  bool
	running sync.WaitGroup
}

func NewCoordinator(config Config) (*Coordinator, error) {
	if config.Loop == nil || config.ActiveTasks == nil || config.Operations == nil || config.SessionTasks == nil || config.Tools == nil || config.Sessions == nil || config.Workspaces == nil || config.Events == nil {
		return nil, errors.New("execution loop, active tasks, task operations, queue, tool services and event publisher are required")
	}
	if config.PrimaryAgent == nil || config.SessionModels == nil || config.Recovery == nil {
		return nil, errors.New("primary agent, session models and execution recovery source are required")
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
		tools:             config.Tools,
		sessions:          config.Sessions,
		workspaces:        config.Workspaces,
		primary:           config.PrimaryAgent,
		sessionModels:     config.SessionModels,
		loop:              config.Loop,
		activeTasks:       config.ActiveTasks,
		operations:        config.Operations,
		sessionTasks:      config.SessionTasks,
		events:            config.Events,
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
func (r *Coordinator) Activate(ctx context.Context, task agentexecution.Execution) error {
	if err := r.enter(ctx); err != nil {
		return err
	}
	defer r.running.Done()
	if !task.Active() {
		return nil
	}
	return r.activate(ctx, task)
}

func (r *Coordinator) activate(ctx context.Context, task agentexecution.Execution) error {
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
		// 恢复可能在 Close 发送取消后才预留身份，补发取消但保留持久化恢复能力。
		r.activeTasks.Stop(task.AgentID, task.ID)
		return errors.New("execution coordinator is closed")
	}
	active, claimed, err := r.activeTasks.TryRun(task.AgentID, task.ID)
	if err != nil || !claimed {
		return err
	}
	r.running.Go(func() { r.run(active.Context, task, active) })
	return nil
}

// ResumeApproved 在审批事实提交后唤醒同一执行；尚未建立协程时正常激活。
func (r *Coordinator) ResumeApproved(ctx context.Context, task agentexecution.Execution) error {
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
	r.activeTasks.StopAll()
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
