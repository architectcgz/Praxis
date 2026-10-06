package agentruntime

import (
	"praxis/internal/contracts"
	turnmodel "praxis/internal/core/turn"

	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// TurnLogger 接收已脱敏的运行时失败，仅用于诊断。
// 不能用它记录 Provider payload 或凭据。
type TurnLogger func(turnmodel.Turn, string, error)

// TurnEventLogger 接收已脱敏的运行时生命周期事件。
type TurnEventLogger func(turnmodel.Turn, string)

type RuntimeConfig struct {
	AgentID          contracts.AgentID
	Messages         MessageStoreResolver
	Runner           TurnRunner
	Logger           TurnLogger
	EventLogger      TurnEventLogger
	LifecycleTimeout time.Duration
}

// Runtime 是单个 Agent 的进程内执行器，活动状态不会跨进程恢复。
type Runtime struct {
	agentID          contracts.AgentID
	messages         MessageStoreResolver
	runner           TurnRunner
	logger           TurnLogger
	eventLogger      TurnEventLogger
	lifecycleTimeout time.Duration

	mu      sync.Mutex
	active  *runtimeTurn
	closed  bool
	running sync.WaitGroup
}

type runtimeTurn struct {
	id            contracts.TurnID
	cancel        context.CancelFunc
	cancelOutcome turnmodel.TurnOutcome
	lifecycle     TurnLifecycle
	ending        bool
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	if config.AgentID == "" {
		return nil, errors.New("target runtime agent id is required")
	}
	if config.Messages == nil {
		return nil, errors.New("target runtime message resolver is required")
	}
	if config.Runner == nil {
		return nil, errors.New("target runtime turn runner is required")
	}
	timeout := config.LifecycleTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	logger := config.Logger
	if logger == nil {
		logger = defaultTurnLogger
	}
	eventLogger := config.EventLogger
	if eventLogger == nil {
		eventLogger = func(turnmodel.Turn, string) {}
	}
	return &Runtime{
		agentID:          config.AgentID,
		messages:         config.Messages,
		runner:           config.Runner,
		logger:           logger,
		eventLogger:      eventLogger,
		lifecycleTimeout: timeout,
	}, nil
}

// Activate 只接受属于当前 Agent 且已持久化的 turn。
// lifecycle 负责执行开始和结束的状态更新；重复激活幂等，仍在执行的另一个 turn 会被拒绝。
func (r *Runtime) Activate(
	ctx context.Context,
	turn turnmodel.Turn,
	lifecycle TurnLifecycle,
) error {
	if ctx == nil {
		return errors.New("target runtime activation context is required")
	}
	if lifecycle == nil {
		return errors.New("target runtime turn lifecycle is required")
	}
	if err := turn.Validate(); err != nil {
		return err
	}
	if turn.AgentID != r.agentID || !turn.Active() {
		return errors.New("target runtime received an ineligible turn")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("target runtime is closed")
	}
	if r.active != nil {
		if r.active.id == turn.ID {
			return nil
		}
		if !r.active.ending {
			return errors.New("target runtime already has an active turn")
		}
	}
	turnCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	active := &runtimeTurn{
		id:            turn.ID,
		cancel:        cancel,
		cancelOutcome: turnmodel.TurnInterrupted,
		lifecycle:     lifecycle,
	}
	r.active = active
	r.running.Go(func() { r.run(turnCtx, turn, active) })
	r.logEvent(turn, "activation accepted")
	return nil
}

// Cancel 通知指定的活动 Turn 停止；返回 true 只表示信号已发送，不表示结束已持久化。
func (r *Runtime) Cancel(
	ctx context.Context,
	turnID contracts.TurnID,
	outcome turnmodel.TurnOutcome,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("target runtime cancellation context is required")
	}
	if outcome != turnmodel.TurnPaused && outcome != turnmodel.TurnInterrupted {
		return false, errors.New("target runtime cancellation outcome is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.id != turnID || r.active.ending {
		return false, nil
	}
	r.active.cancelOutcome = outcome
	r.active.cancel()
	r.logEventLocked(turnID, "cancellation requested")
	return true, nil
}

// Close 取消活动执行，并仅等待到调用方给定的 deadline。
func (r *Runtime) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("target runtime close context is required")
	}
	r.mu.Lock()
	r.closed = true
	active := r.active
	if active != nil {
		active.cancelOutcome = turnmodel.TurnInterrupted
		active.cancel()
	}
	r.mu.Unlock()
	// 下一轮可能在上一轮终态回调内启动；关闭必须同时等待所有结束事务和回调。
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
	turn turnmodel.Turn,
	active *runtimeTurn,
) {
	r.logEvent(turn, "turn started")
	defer r.finishTurn(active)
	defer active.cancel()

	if !r.startTurn(turn, active) {
		return
	}

	messages, err := r.messages(ctx, turn.SessionID, turn.AgentID)
	if err != nil || messages == nil {
		if err == nil {
			err = errors.New("message resolver returned a nil store")
		}
		r.logError(turn, "open messages", err)
		outcome, code, message := r.executionResult(ctx, active, turnmodel.TurnFailed, contracts.TurnFailureRuntimeFailed, err)
		r.endTurn(turn, active, outcome, code, message)
		return
	}

	outcome, failureCode, failureMessage := r.execute(ctx, turn, messages, active)
	if !r.endTurn(turn, active, outcome, failureCode, failureMessage) {
		return
	}
	r.logEvent(turn, "turn ended")
}

// finishTurn 仅释放属于本轮的活动槽位，不清除终态回调已启动的下一轮。
func (r *Runtime) finishTurn(active *runtimeTurn) {
	r.mu.Lock()
	if r.active == active {
		r.active = nil
	}
	r.mu.Unlock()
}

func (r *Runtime) startTurn(
	turn turnmodel.Turn,
	active *runtimeTurn,
) bool {
	startContext, cancelStart := r.lifecycleContext()
	defer cancelStart()
	if err := active.lifecycle.StartRuntimeTurn(startContext, turn.ID); err != nil {
		r.logError(turn, "start product turn", err)
		return false
	}
	return true
}

// execute 运行 Provider/tool loop，并把取消转换为可持久化的结果。
// runner 不拥有持久化生命周期，因此 context 取消后仍会交由业务服务保存结束状态。
func (r *Runtime) execute(
	ctx context.Context,
	turn turnmodel.Turn,
	store TurnMessageStore,
	active *runtimeTurn,
) (turnmodel.TurnOutcome, contracts.TurnFailureCode, string) {
	outcome, failureCode, err := r.runner.RunWithSession(ctx, turn, store)
	r.logEvent(turn, "provider completed")
	if err != nil {
		r.logError(turn, "run provider", err)
	}
	return r.executionResult(ctx, active, outcome, failureCode, err)
}

// executionResult 在同一把锁内读取取消结果；持久化控制命令由结束事务最终仲裁。
func (r *Runtime) executionResult(
	ctx context.Context,
	active *runtimeTurn,
	outcome turnmodel.TurnOutcome,
	failureCode contracts.TurnFailureCode,
	err error,
) (turnmodel.TurnOutcome, contracts.TurnFailureCode, string) {
	failureMessage := contracts.TurnFailureMessage(failureCode, err)
	r.mu.Lock()
	if ctx.Err() != nil {
		outcome = active.cancelOutcome
		failureCode = contracts.TurnFailureRuntimeCancelled
		failureMessage = ""
	} else if err != nil {
		outcome = turnmodel.TurnFailed
		if failureCode == "" {
			failureCode = contracts.TurnFailureRuntimeFailed
		}
	}
	r.mu.Unlock()
	outcome, failureCode = normalizeOutcome(outcome, failureCode)
	if failureCode != contracts.TurnFailureProvider {
		failureMessage = ""
	}
	return outcome, failureCode, failureMessage
}

func (r *Runtime) endTurn(
	turn turnmodel.Turn,
	active *runtimeTurn,
	outcome turnmodel.TurnOutcome,
	failureCode contracts.TurnFailureCode,
	failureMessage string,
) bool {
	// runner 已返回，允许已通过持久化准入的下一轮接管；同一 Turn 的重复激活仍保持幂等。
	r.mu.Lock()
	active.ending = true
	r.mu.Unlock()
	endContext, cancelEnd := r.lifecycleContext()
	defer cancelEnd()
	if err := active.lifecycle.EndRuntimeTurn(
		endContext,
		turn.ID,
		outcome,
		failureCode,
		failureMessage,
	); err != nil {
		r.logError(turn, "end product turn", err)
		return false
	}
	return true
}

func normalizeOutcome(
	outcome turnmodel.TurnOutcome,
	failureCode contracts.TurnFailureCode,
) (turnmodel.TurnOutcome, contracts.TurnFailureCode) {
	if !knownOutcome(outcome) {
		outcome = turnmodel.TurnFailed
		failureCode = contracts.TurnFailureRuntimeInvalid
	}
	if outcome == turnmodel.TurnFailed && failureCode == "" {
		failureCode = contracts.TurnFailureRuntimeFailed
	}
	if !failureCode.Valid() {
		outcome = turnmodel.TurnFailed
		failureCode = contracts.TurnFailureRuntimeFailed
	}
	return outcome, failureCode
}

func (r *Runtime) logError(turn turnmodel.Turn, stage string, err error) {
	if err == nil {
		return
	}
	r.logger(turn, stage, err)
}

func (r *Runtime) logEvent(turn turnmodel.Turn, stage string) {
	r.eventLogger(turn, stage)
}

func (r *Runtime) logEventLocked(turnID contracts.TurnID, stage string) {
	r.eventLogger(turnmodel.Turn{ID: turnID, AgentID: r.agentID}, stage)
}

func defaultTurnLogger(turn turnmodel.Turn, stage string, err error) {
	if err == nil {
		return
	}
	log.Printf("praxis turn %s %s failed: %v", turn.ID, stage, err)
}

func (r *Runtime) lifecycleContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.lifecycleTimeout)
}

func knownOutcome(outcome turnmodel.TurnOutcome) bool {
	switch outcome {
	case turnmodel.TurnCompleted, turnmodel.TurnYielded, turnmodel.TurnPaused,
		turnmodel.TurnFailed, turnmodel.TurnInterrupted:
		return true
	default:
		return false
	}
}
