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
	AgentID           contracts.AgentID
	Messages          MessageStoreResolver
	Runner            TurnRunner
	Logger            TurnLogger
	EventLogger       TurnEventLogger
	EventObserver     AgentEventObserver
	SettlementTimeout time.Duration
}

// Runtime 是单个 Agent 的进程内执行器，活动状态不会跨进程恢复。
type Runtime struct {
	agentID           contracts.AgentID
	messages          MessageStoreResolver
	runner            TurnRunner
	logger            TurnLogger
	eventLogger       TurnEventLogger
	eventObserver     AgentEventObserver
	settlementTimeout time.Duration

	mu     sync.Mutex
	active *runtimeTurn
	closed bool
}

type runtimeTurn struct {
	id            contracts.TurnID
	cancel        context.CancelFunc
	done          chan struct{}
	cancelOutcome turnmodel.TurnOutcome
	lifecycle     TurnLifecycle
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
	timeout := config.SettlementTimeout
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
		agentID:           config.AgentID,
		messages:          config.Messages,
		runner:            config.Runner,
		logger:            logger,
		eventLogger:       eventLogger,
		eventObserver:     config.EventObserver,
		settlementTimeout: timeout,
	}, nil
}

// Activate 只接受属于当前 Agent 且已持久化的 turn。
// lifecycle 负责执行开始和结束的状态更新；重复激活幂等，另一个活动 turn 会被拒绝。
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
		return errors.New("target runtime already has an active turn")
	}
	turnCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	active := &runtimeTurn{
		id:            turn.ID,
		cancel:        cancel,
		done:          make(chan struct{}),
		cancelOutcome: turnmodel.TurnInterrupted,
		lifecycle:     lifecycle,
	}
	r.active = active
	go r.run(turnCtx, turn, active)
	r.logEvent(turn, "activation accepted")
	return nil
}

func (r *Runtime) Cancel(
	ctx context.Context,
	turnID contracts.TurnID,
	outcome turnmodel.TurnOutcome,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("target runtime cancellation context is required")
	}
	if !knownOutcome(outcome) {
		return false, errors.New("target runtime cancellation outcome is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.id != turnID {
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
	if active == nil {
		return nil
	}
	select {
	case <-active.done:
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

	messages, err := r.messages(ctx, turn.SessionID, turn.AgentID)
	if err != nil || messages == nil {
		if err == nil {
			err = errors.New("message resolver returned a nil store")
		}
		r.logError(turn, "open messages", err)
		return
	}

	if !r.startTurn(turn, active) {
		return
	}

	outcome, failureCode, failureMessage := r.execute(ctx, turn, messages, active)
	if !r.settleTurn(turn, active, outcome, failureCode, failureMessage) {
		return
	}
	r.logEvent(turn, "turn settled")
}

// finishTurn 在 runner 接收下一个 turn 前释放活动槽位。
// done 信号在同一把锁内关闭，避免 Close 和 settlement 观察到释放一半的状态。
func (r *Runtime) finishTurn(active *runtimeTurn) {
	r.mu.Lock()
	if r.active == active {
		r.active = nil
	}
	close(active.done)
	r.mu.Unlock()
}

func (r *Runtime) startTurn(
	turn turnmodel.Turn,
	active *runtimeTurn,
) bool {
	startContext, cancelStart := r.settlementContext()
	defer cancelStart()
	if err := active.lifecycle.StartRuntimeTurn(startContext, turn.ID); err != nil {
		r.logError(turn, "start product turn", err)
		return false
	}
	return true
}

// execute 运行 Provider/tool loop，并把取消转换为可持久化的结果。
// runner 不拥有 settlement，因此 context 取消后仍会交由业务服务结算。
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
	failureMessage := contracts.TurnFailureMessage(failureCode, err)
	if ctx.Err() != nil {
		r.mu.Lock()
		outcome = active.cancelOutcome
		r.mu.Unlock()
		failureCode = contracts.TurnFailureRuntimeCancelled
		failureMessage = ""
	} else if err != nil {
		outcome = turnmodel.TurnFailed
		if failureCode == "" {
			failureCode = contracts.TurnFailureRuntimeFailed
		}
	}
	outcome, failureCode = normalizeOutcome(outcome, failureCode)
	if failureCode != contracts.TurnFailureProvider {
		failureMessage = ""
	}
	return outcome, failureCode, failureMessage
}

func (r *Runtime) settleTurn(
	turn turnmodel.Turn,
	active *runtimeTurn,
	outcome turnmodel.TurnOutcome,
	failureCode contracts.TurnFailureCode,
	failureMessage string,
) bool {
	settlementContext, cancelSettlement := r.settlementContext()
	defer cancelSettlement()
	if err := active.lifecycle.SettleRuntimeTurn(
		settlementContext,
		turn.ID,
		outcome,
		failureCode,
		failureMessage,
	); err != nil {
		r.logError(turn, "settle product turn", err)
		return false
	}
	if r.eventObserver != nil {
		r.eventObserver(AgentEvent{
			Kind:      AgentEventTurnSettled,
			SessionID: turn.SessionID,
			AgentID:   turn.AgentID,
			TurnID:    turn.ID,
		})
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

func (r *Runtime) settlementContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.settlementTimeout)
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
