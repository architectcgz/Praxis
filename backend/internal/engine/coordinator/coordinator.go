// Package coordinator 协调进程内 Run 执行、审批等待和内存输入队列交接。
package coordinator

import (
	"context"
	"errors"
	"maps"
	"praxis/internal/engine/approval"
	"praxis/internal/engine/hook"
	"praxis/internal/engine/loop"
	runmodel "praxis/internal/engine/run"
	sessionmodel "praxis/internal/engine/session"
	"praxis/internal/engine/toolexecution"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/model"
	"praxis/internal/project"
	"praxis/internal/security"
	"praxis/internal/task"
	"slices"
	"sync"
	"time"
)

var (
	errCoordinatorClosed = errors.New("execution coordinator is closed")
	errRunInterrupted    = runmodel.ErrInterrupted
)

type ProjectStore interface {
	Get(context.Context, identity.ProjectID) (project.Project, error)
}

type SessionStore interface {
	Get(context.Context, identity.SessionID) (sessionmodel.SessionInfo, error)
	SetModelChoice(context.Context, identity.SessionID, model.Choice, time.Time) error
	// UpdateTitle 仅当原标题等于 expectedTitle 时原子更新；未匹配时返回 ErrRequestConflict。
	UpdateTitle(ctx context.Context, id identity.SessionID, expectedTitle, title string, at time.Time) error
	SetApprovalMode(context.Context, identity.SessionID, *security.ApprovalMode, time.Time) (sessionmodel.SessionInfo, error)
}

// SessionLoader 打开并恢复会话历史，仅由负责运行期会话生命周期的协调器调用。
type SessionLoader interface {
	Load(context.Context, identity.SessionID) (*sessionmodel.Session, error)
}

type Config struct {
	Tools             *toolexecution.Executor
	Approvals         *approval.Gate
	LoadConfig        func(context.Context) (*sessionmodel.Configuration, error)
	SessionRecords    SessionStore
	SessionLoader     SessionLoader
	Projects          ProjectStore
	Loop              *loop.Runner
	Emitter           *event.Emitter
	RunDependencies   runmodel.Dependencies
	HookDispatcher    *hook.Dispatcher
	Logger            *logging.Logger
	TransitionTimeout time.Duration
	ShutdownTimeout   time.Duration
	ActiveSubagents   int
}

// Coordinator 统一拥有队列调度、执行忙闲状态和取消句柄。
type Coordinator struct {
	tools             *toolexecution.Executor
	approvals         *approval.Gate
	loadConfig        func(context.Context) (*sessionmodel.Configuration, error)
	sessionRecords    SessionStore
	sessionLoader     SessionLoader
	projects          ProjectStore
	loop              *loop.Runner
	emitter           *event.Emitter
	runDependencies   runmodel.Dependencies
	hookDispatcher    *hook.Dispatcher
	logger            *logging.Logger
	transitionTimeout time.Duration
	shutdownTimeout   time.Duration
	activeSubagents   int

	// mu 保护会话调度表、命令信号量、任务队列及其活动占位；
	// 存储读写、会话数据访问与 Run 执行均不持有 mu。
	mu              sync.Mutex
	closed          bool
	sessionCommands map[identity.SessionID]*sessionCommand
	schedules       map[identity.SessionID]*sessionSchedule
	running         sync.WaitGroup
}

// sessionCommand 串行化同一会话的命令与调度登记，不覆盖模型、工具执行和运行结算。
// refs 包含持有者与等待者，归零后移除登记，不依赖已加载实例的存续时间。
type sessionCommand struct {
	held chan struct{}
	refs int
}

func (s *Coordinator) lockSession(ctx context.Context, id identity.SessionID) (func(), error) {
	ctx, cancel := context.WithTimeout(ctx, s.transitionTimeout)
	defer cancel()
	s.mu.Lock()
	command := s.sessionCommands[id]
	if command == nil {
		command = &sessionCommand{held: make(chan struct{}, 1)}
		s.sessionCommands[id] = command
	}
	command.refs++
	s.mu.Unlock()

	select {
	case command.held <- struct{}{}:
		if err := ctx.Err(); err != nil {
			<-command.held
			s.releaseSessionCommand(id, command)
			return nil, err
		}
		return func() {
			<-command.held
			s.releaseSessionCommand(id, command)
		}, nil
	case <-ctx.Done():
		s.releaseSessionCommand(id, command)
		return nil, ctx.Err()
	}
}

func (s *Coordinator) releaseSessionCommand(id identity.SessionID, command *sessionCommand) {
	s.mu.Lock()
	defer s.mu.Unlock()
	command.refs--
	if command.refs == 0 {
		delete(s.sessionCommands, id)
	}
}

// activeRun 是所属会话或 Parent 持有的控制句柄，不提供运行状态查询。
type activeRun struct {
	run     *runmodel.Run // 在提交准备事实前登记，由 mu 保护。
	parent  *runmodel.Run
	onDone  func() error
	session *sessionmodel.Session

	config *sessionmodel.Configuration
	ctx    context.Context
	cancel context.CancelCauseFunc
	done   chan struct{}
}

func newActiveRun(ctx context.Context, session *sessionmodel.Session) *activeRun {
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	return &activeRun{
		session: session,
		ctx:     runCtx,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
}

func NewCoordinator(config Config) *Coordinator {
	return &Coordinator{
		tools:             config.Tools,
		approvals:         config.Approvals,
		loadConfig:        config.LoadConfig,
		sessionRecords:    config.SessionRecords,
		sessionLoader:     config.SessionLoader,
		projects:          config.Projects,
		loop:              config.Loop,
		emitter:           config.Emitter,
		sessionCommands:   make(map[identity.SessionID]*sessionCommand),
		schedules:         make(map[identity.SessionID]*sessionSchedule),
		runDependencies:   config.RunDependencies,
		hookDispatcher:    config.HookDispatcher,
		logger:            config.Logger,
		transitionTimeout: config.TransitionTimeout,
		shutdownTimeout:   config.ShutdownTimeout,
		activeSubagents:   config.ActiveSubagents,
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
func (r *Coordinator) launchLocked(run *runmodel.Run, active *activeRun) error {
	if r.closed {
		return errCoordinatorClosed
	}
	run.BindCancellation(active.cancel)
	r.running.Go(func() { r.run(active.ctx, run, active) })
	return nil
}

// Close 拒绝新命令，取消所有运行，并等待已接纳命令、准备和结算操作退出。
// 超时后资源仍由 Application 保留，后续 Close 可以继续等待。
func (r *Coordinator) Close(ctx context.Context) error {
	r.mu.Lock()
	r.closed = true
	executions := make([]*activeRun, 0, len(r.schedules))
	for _, schedule := range r.schedules {
		if schedule.active != nil {
			executions = append(executions, schedule.active)
		}
	}
	r.mu.Unlock()
	for _, active := range executions {
		active.cancel(context.Canceled)
	}
	done := make(chan struct{})
	go func() { r.running.Wait(); close(done) }()
	select {
	case <-done:
		r.mu.Lock()
		ids := slices.Collect(maps.Keys(r.schedules))
		clear(r.sessionCommands)
		r.mu.Unlock()
		var closeErr error
		for _, id := range ids {
			if ctx.Err() != nil {
				break
			}
			closeErr = errors.Join(closeErr, r.releaseSession(ctx, id))
		}
		return errors.Join(closeErr, ctx.Err())
	case <-ctx.Done():
		return ctx.Err()
	}
}

type sessionSchedule struct {
	sessionID identity.SessionID
	session   *sessionmodel.Session
	queue     task.TaskQueue
	active    *activeRun
	closing   bool
}
