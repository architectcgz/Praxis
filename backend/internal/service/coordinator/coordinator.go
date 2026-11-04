// Package coordinator 协调进程内 Run 执行、审批等待和内存输入队列交接。
package coordinator

import (
	"context"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	sessionmodel "praxis/internal/core/session"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/repository"
	sessionservice "praxis/internal/service/session"
	"sync"
	"time"
)

type Config struct {
	Tools             *executionmodel.Executor
	Sessions          *sessionservice.Service
	SessionRecords    repository.Session
	Projects          repository.Project
	Loop              *loop.Runner
	Operations        RunOperations
	HookDispatcher    *hook.Dispatcher
	Logger            *logging.Logger
	TransitionTimeout time.Duration
}

// Coordinator 统一拥有队列调度、执行忙闲状态和取消句柄。
type Coordinator struct {
	tools             *executionmodel.Executor
	sessions          *sessionservice.Service
	sessionRecords    repository.Session
	projects          repository.Project
	loop              *loop.Runner
	operations        RunOperations
	hookDispatcher    *hook.Dispatcher
	logger            *logging.Logger
	transitionTimeout time.Duration

	// 领取队首与标记忙碌在同一临界区完成；准备、存储和模型调用都在锁外执行。
	mu            sync.Mutex
	closed        bool
	runs          map[runKey]*activeRun
	pages         map[string]identity.SessionID
	bindings      map[string]*sessionBinding
	branchOwners  map[branchKey]string
	branchResults map[string]string
	running       sync.WaitGroup
}

// activeRun 从准备队首开始存在，直到结算成功才移除；字段变更由 Coordinator.mu 保护。
type activeRun struct {
	sessionID identity.SessionID
	branchID  identity.BranchID
	bindingID string
	run       *agentrun.Run
	config    *sessionservice.Configuration
	runID     identity.RunID
	ctx       context.Context
	cancel    context.CancelCauseFunc
	ending    bool
	// 停止后新提交的输入可请求收尾后重新调度，不把旧队列当作继续执行的意图。
	scheduleRequested bool
	ready             chan struct{}
	toolApprovalWake  chan struct{}
}

func newActiveRun(ctx context.Context, sessionID identity.SessionID) *activeRun {
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	return &activeRun{
		sessionID:        sessionID,
		ctx:              runCtx,
		cancel:           cancel,
		ready:            make(chan struct{}),
		toolApprovalWake: make(chan struct{}, 1),
	}
}

func NewCoordinator(config Config) *Coordinator {
	return &Coordinator{
		tools:             config.Tools,
		sessions:          config.Sessions,
		sessionRecords:    config.SessionRecords,
		projects:          config.Projects,
		loop:              config.Loop,
		runs:              make(map[runKey]*activeRun),
		pages:             make(map[string]identity.SessionID),
		bindings:          make(map[string]*sessionBinding),
		branchOwners:      make(map[branchKey]string),
		branchResults:     make(map[string]string),
		operations:        config.Operations,
		hookDispatcher:    config.HookDispatcher,
		logger:            config.Logger,
		transitionTimeout: config.TransitionTimeout,
	}
}

// enter 将前台命令和队列准备计入关闭等待，禁止关闭存储后继续写入。
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
func (r *Coordinator) launchLocked(run *agentrun.Run, active *activeRun) error {
	if r.closed {
		return errCoordinatorClosed
	}
	active.run = run
	active.runID = run.Snapshot().ID
	close(active.ready)
	r.running.Go(func() { r.run(active.ctx, run, active) })
	return nil
}

// Close 拒绝新命令，取消所有运行，并等待已接纳命令、准备和结算操作退出。
// 超时后资源仍由 Application 保留，后续 Close 可以继续等待。
func (r *Coordinator) Close(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	for _, active := range r.runs {
		active.cancel(context.Canceled)
	}
	r.mu.Unlock()
	done := make(chan struct{})
	go func() { r.running.Wait(); close(done) }()
	select {
	case <-done:
		r.mu.Lock()
		for _, binding := range r.bindings {
			binding.queue.Clear()
		}
		clear(r.bindings)
		clear(r.pages)
		clear(r.branchOwners)
		clear(r.branchResults)
		r.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Coordinator) transitionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.transitionTimeout)
}

type runKey struct {
	agentID  identity.AgentID
	branchID identity.BranchID
}
type branchKey struct {
	sessionID identity.SessionID
	branchID  identity.BranchID
}
type sessionBinding struct {
	id        string
	sessionID identity.SessionID
	branchID  identity.BranchID
	session   *sessionmodel.Session
	queue     sessionmodel.TaskQueue
	closing   bool
}
