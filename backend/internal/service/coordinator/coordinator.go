// Package coordinator 协调进程内 Execution 执行、审批等待和内存输入队列交接。
package coordinator

import (
	"context"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/repository"
	sessionservice "praxis/internal/service/session"
	toolservice "praxis/internal/service/toolexecution"
	"sync"
	"time"
)

type Config struct {
	ConfigMu          *sync.RWMutex
	Recovery          repository.AgentExecution
	Tools             *executionmodel.Executor
	ToolRecords       *toolservice.Service
	Sessions          *sessionservice.Service
	SessionRecords    repository.Session
	Workspaces        repository.Workspace
	MainAgent         func(context.Context, identity.SessionID) (*agentmodel.Agent, error)
	Loop              *loop.Runner
	Operations        ExecutionOperations
	HookDispatcher    *hook.Dispatcher
	Logger            *logging.Logger
	TransitionTimeout time.Duration
}

// Coordinator 统一拥有队列调度、执行忙闲状态和取消句柄。
type Coordinator struct {
	configMu          *sync.RWMutex
	recovery          repository.AgentExecution
	tools             *executionmodel.Executor
	toolRecords       *toolservice.Service
	sessions          *sessionservice.Service
	sessionRecords    repository.Session
	workspaces        repository.Workspace
	mainAgent         func(context.Context, identity.SessionID) (*agentmodel.Agent, error)
	restored          bool
	restoring         bool
	loop              *loop.Runner
	operations        ExecutionOperations
	hookDispatcher    *hook.Dispatcher
	logger            *logging.Logger
	transitionTimeout time.Duration

	// 领取队首与标记忙碌在同一临界区完成；准备、存储和模型调用都在锁外执行。
	mu         sync.Mutex
	closed     bool
	executions map[identity.AgentID]*activeExecution
	running    sync.WaitGroup
}

// activeExecution 从准备队首开始存在，直到结算成功才移除；字段变更由 Coordinator.mu 保护。
type activeExecution struct {
	run         *agentexecution.Run
	executionID identity.AgentExecutionID
	ctx         context.Context
	cancel      context.CancelCauseFunc
	ending      bool
	// 控制后新提交的输入可请求收尾后重新调度，不把旧队列当作继续执行的意图。
	resumeRequested  bool
	ready            chan struct{}
	toolApprovalWake chan struct{}
}

func newActiveExecution(ctx context.Context) *activeExecution {
	executionCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	return &activeExecution{
		ctx:              executionCtx,
		cancel:           cancel,
		ready:            make(chan struct{}),
		toolApprovalWake: make(chan struct{}, 1),
	}
}

func NewCoordinator(config Config) *Coordinator {
	return &Coordinator{
		configMu:          config.ConfigMu,
		recovery:          config.Recovery,
		tools:             config.Tools,
		toolRecords:       config.ToolRecords,
		sessions:          config.Sessions,
		sessionRecords:    config.SessionRecords,
		workspaces:        config.Workspaces,
		mainAgent:         config.MainAgent,
		loop:              config.Loop,
		executions:        make(map[identity.AgentID]*activeExecution),
		operations:        config.Operations,
		hookDispatcher:    config.HookDispatcher,
		logger:            config.Logger,
		transitionTimeout: config.TransitionTimeout,
	}
}

// enter 将前台恢复和队列准备计入关闭等待，禁止关闭 JSONL 后继续写入。
func (r *Coordinator) enter() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errCoordinatorClosed
	}
	r.running.Add(1)
	return nil
}

// launchLocked 只接收已由调度器标记忙碌的执行，不再竞争槽位；调用方持有 mu。
func (r *Coordinator) launchLocked(run *agentexecution.Run, active *activeExecution) error {
	if r.closed {
		return errCoordinatorClosed
	}
	active.run = run
	active.executionID = run.Snapshot().ID
	close(active.ready)
	r.running.Go(func() { r.run(active.ctx, run, active) })
	return nil
}

// Close 拒绝新任务，取消所有运行，并等待准备、恢复和结算操作退出。
// 超时后资源仍由 Application 保留，后续 Close 可以继续等待。
func (r *Coordinator) Close(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	for _, active := range r.executions {
		active.cancel(context.Canceled)
	}
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
