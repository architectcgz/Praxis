// Package coordinator 协调进程内 Run 执行、审批等待和内存输入队列交接。
package coordinator

import (
	"context"
	"errors"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	executionmodel "praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"praxis/internal/logging"
	"praxis/internal/repository"
	"sync"
	"time"
)

var (
	errCoordinatorClosed = errors.New("execution coordinator is closed")
	errRunInterrupted    = agentrun.ErrInterrupted
)

type Config struct {
	Tools             *executionmodel.Executor
	LoadConfig        func(context.Context) (*sessionmodel.Configuration, error)
	SessionRecords    repository.Session
	Projects          repository.Project
	Loop              *loop.Runner
	RunDependencies   agentrun.Dependencies
	HookDispatcher    *hook.Dispatcher
	Logger            *logging.Logger
	TransitionTimeout time.Duration
}

// Coordinator 统一拥有队列调度、执行忙闲状态和取消句柄。
type Coordinator struct {
	tools             *executionmodel.Executor
	loadConfig        func(context.Context) (*sessionmodel.Configuration, error)
	sessionRecords    repository.Session
	projects          repository.Project
	loop              *loop.Runner
	runDependencies   agentrun.Dependencies
	hookDispatcher    *hook.Dispatcher
	logger            *logging.Logger
	transitionTimeout time.Duration

	// mu 保护已加载会话、队列绑定、会话命令信号量及会话执行句柄；
	// 存储读写、会话数据访问与 Run 执行均不持有 mu。
	mu              sync.Mutex
	closed          bool
	registry        sessionRegistry
	sessionCommands map[identity.SessionID]*sessionCommand
	schedules       map[identity.SessionID]*sessionSchedule
	running         sync.WaitGroup
}

type sessionRegistry struct {
	values map[identity.SessionID]*sessionmodel.Session
}

func (r *sessionRegistry) get(id identity.SessionID) *sessionmodel.Session {
	return r.values[id]
}

func (r *sessionRegistry) add(id identity.SessionID, s *sessionmodel.Session) {
	if r.values == nil {
		r.values = make(map[identity.SessionID]*sessionmodel.Session)
	}
	r.values[id] = s
}

func (r *sessionRegistry) remove(id identity.SessionID) {
	delete(r.values, id)
}

func (r *sessionRegistry) clear() {
	clear(r.values)
}

// sessionCommand 串行化同一会话的命令与调度登记，不覆盖模型、工具执行和运行结算。
// refs 包含持有者与等待者，归零后移除登记，不依赖已加载实例的存续时间。
type sessionCommand struct {
	held chan struct{}
	refs int
}

func (s *Coordinator) lockSession(ctx context.Context, id identity.SessionID) (func(), error) {
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
	run     *agentrun.Run // 会话调度绑定的主 Run，在提交准备事实前登记，由 mu 保护。
	parent  *agentrun.Run
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
		loadConfig:        config.LoadConfig,
		sessionRecords:    config.SessionRecords,
		projects:          config.Projects,
		loop:              config.Loop,
		sessionCommands:   make(map[identity.SessionID]*sessionCommand),
		schedules:         make(map[identity.SessionID]*sessionSchedule),
		runDependencies:   config.RunDependencies,
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
		clear(r.schedules)
		clear(r.sessionCommands)
		r.registry.clear()
		r.mu.Unlock()
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (r *Coordinator) transitionContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.transitionTimeout)
}

type sessionSchedule struct {
	sessionID identity.SessionID
	session   *sessionmodel.Session
	queue     taskmodel.TaskQueue
	active    *activeRun
	closing   bool
	reloading bool
}
