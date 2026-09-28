package agent

import (
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
	"errors"
	"log"
	"sync"
	"time"

	runtimecontract "praxis/internal/runtime"
)

// SessionResolver 返回指定 Agent 唯一的 JSONL writer。
// 该接口不允许 runtime 读取其他 Agent 的 transcript。
type SessionResolver func(contracts.SessionID, contracts.AgentID) (runtimecontract.TranscriptStore, error)

type SessionHeaderResolver func(
	context.Context,
	executionmodel.AgentExecution,
) (runtimecontract.AgentSessionHeader, error)

// ExecutionRunner 是 provider/tool 边界。它接收持久化的不可变执行快照
// 和 Agent 拥有的 transcript 存储接口，只返回稳定的结算结果。
type ExecutionRunner interface {
	RunWithSession(
		context.Context,
		executionmodel.AgentExecution,
		runtimecontract.TranscriptStore,
	) (executionmodel.ExecutionOutcome, contracts.ExecutionFailureCode, error)
}

// ExecutionLogger 接收已脱敏的运行时失败，仅用于诊断。
// 不能用它记录 Provider payload 或凭据。
type ExecutionLogger func(executionmodel.AgentExecution, string, error)

// ExecutionEventLogger 接收已脱敏的运行时生命周期事件。
type ExecutionEventLogger func(executionmodel.AgentExecution, string)

type RuntimeConfig struct {
	AgentID           contracts.AgentID
	Sessions          SessionResolver
	Header            SessionHeaderResolver
	Runner            ExecutionRunner
	Logger            ExecutionLogger
	EventLogger       ExecutionEventLogger
	EventObserver     AgentEventObserver
	SettlementTimeout time.Duration
}

// Runtime 是单个 Agent 的进程内执行器，活动状态不会跨进程恢复。
type Runtime struct {
	agentID           contracts.AgentID
	sessions          SessionResolver
	header            SessionHeaderResolver
	runner            ExecutionRunner
	logger            ExecutionLogger
	eventLogger       ExecutionEventLogger
	eventObserver     AgentEventObserver
	settlementTimeout time.Duration

	mu     sync.Mutex
	active *runtimeExecution
	closed bool
}

type runtimeExecution struct {
	id            contracts.AgentExecutionID
	cancel        context.CancelFunc
	done          chan struct{}
	cancelOutcome executionmodel.ExecutionOutcome
	lifecycle     runtimecontract.ExecutionLifecycle
}

func NewRuntime(config RuntimeConfig) (*Runtime, error) {
	if config.AgentID == "" {
		return nil, errors.New("target runtime agent id is required")
	}
	if config.Sessions == nil {
		return nil, errors.New("target runtime session resolver is required")
	}
	if config.Runner == nil {
		return nil, errors.New("target runtime execution runner is required")
	}
	timeout := config.SettlementTimeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	logger := config.Logger
	if logger == nil {
		logger = defaultExecutionLogger
	}
	eventLogger := config.EventLogger
	if eventLogger == nil {
		eventLogger = func(executionmodel.AgentExecution, string) {}
	}
	return &Runtime{
		agentID:           config.AgentID,
		sessions:          config.Sessions,
		header:            config.Header,
		runner:            config.Runner,
		logger:            logger,
		eventLogger:       eventLogger,
		eventObserver:     config.EventObserver,
		settlementTimeout: timeout,
	}, nil
}

// Activate 只接受属于当前 Agent 且已持久化的 execution。
// lifecycle 只负责该 execution 的 receipt 交接；重复激活幂等，另一个活动 execution 会被拒绝。
func (r *Runtime) Activate(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	lifecycle runtimecontract.ExecutionLifecycle,
) error {
	if ctx == nil {
		return errors.New("target runtime activation context is required")
	}
	if lifecycle == nil {
		return errors.New("target runtime execution lifecycle is required")
	}
	if err := execution.Validate(); err != nil {
		return err
	}
	if execution.AgentID != r.agentID || !execution.Active() {
		return errors.New("target runtime received an ineligible execution")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return errors.New("target runtime is closed")
	}
	if r.active != nil {
		if r.active.id == execution.ID {
			return nil
		}
		return errors.New("target runtime already has an active execution")
	}
	executionCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
	active := &runtimeExecution{
		id:            execution.ID,
		cancel:        cancel,
		done:          make(chan struct{}),
		cancelOutcome: executionmodel.ExecutionInterrupted,
		lifecycle:     lifecycle,
	}
	r.active = active
	go r.run(executionCtx, execution, active)
	r.logEvent(execution, "activation accepted")
	return nil
}

func (r *Runtime) Cancel(
	ctx context.Context,
	executionID contracts.AgentExecutionID,
	outcome executionmodel.ExecutionOutcome,
) (bool, error) {
	if ctx == nil {
		return false, errors.New("target runtime cancellation context is required")
	}
	if !knownOutcome(outcome) {
		return false, errors.New("target runtime cancellation outcome is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.id != executionID {
		return false, nil
	}
	r.active.cancelOutcome = outcome
	r.active.cancel()
	r.logEventLocked(executionID, "cancellation requested")
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
		active.cancelOutcome = executionmodel.ExecutionInterrupted
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
	execution executionmodel.AgentExecution,
	active *runtimeExecution,
) {
	r.logEvent(execution, "execution started")
	defer r.finishExecution(active)

	store, ok := r.openTranscript(ctx, execution)
	if !ok {
		return
	}
	defer func() { _ = store.Close(context.Background()) }()

	if !r.confirmExecutionStart(ctx, execution, store, active) {
		return
	}

	outcome, failureCode := r.execute(ctx, execution, store, active)
	if !r.settleExecution(ctx, execution, store, active, outcome, failureCode) {
		return
	}
	r.logEvent(execution, "execution settled")
}

// finishExecution 在 runner 接收下一个 execution 前释放活动槽位。
// done 信号在同一把锁内关闭，避免 Close 和 settlement 观察到释放一半的状态。
func (r *Runtime) finishExecution(active *runtimeExecution) {
	r.mu.Lock()
	if r.active == active {
		r.active = nil
	}
	close(active.done)
	r.mu.Unlock()
}

// openTranscript 解析并初始化 Agent transcript；失败时停止当前执行，等待人工控制。
func (r *Runtime) openTranscript(
	ctx context.Context,
	execution executionmodel.AgentExecution,
) (runtimecontract.TranscriptStore, bool) {
	store, err := r.sessions(execution.SessionID, execution.AgentID)
	if err != nil || store == nil {
		if err == nil {
			err = errors.New("session resolver returned a nil store")
		}
		r.logError(execution, "open session", err)
		return nil, false
	}
	if r.header == nil {
		return store, true
	}
	header, err := r.header(ctx, execution)
	if err != nil {
		r.logError(execution, "resolve session header", err)
		return nil, false
	}
	if err := store.Initialize(ctx, header); err != nil {
		r.logError(execution, "initialize session", err)
		return nil, false
	}
	return store, true
}

func (r *Runtime) confirmExecutionStart(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	store runtimecontract.TranscriptStore,
	active *runtimeExecution,
) bool {
	start, err := store.AppendExecutionStart(ctx, execution)
	if err != nil {
		r.logError(execution, "append execution start", err)
		return false
	}
	startContext, cancelStart := r.settlementContext()
	err = active.lifecycle.ConfirmExecutionStart(startContext, start)
	cancelStart()
	if err != nil {
		r.logError(execution, "confirm execution start", err)
		return false
	}
	return true
}

// execute 运行 Provider/tool loop，并把取消转换为可持久化的结果。
// runner 不拥有 settlement，因此 context 取消后仍会生成有效 receipt 结果。
func (r *Runtime) execute(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	store runtimecontract.TranscriptStore,
	active *runtimeExecution,
) (executionmodel.ExecutionOutcome, contracts.ExecutionFailureCode) {
	outcome, failureCode, err := r.runner.RunWithSession(ctx, execution, store)
	r.logEvent(execution, "provider completed")
	if err != nil {
		r.logError(execution, "run provider", err)
	}
	if ctx.Err() != nil {
		r.mu.Lock()
		outcome = active.cancelOutcome
		r.mu.Unlock()
		failureCode = contracts.ExecutionFailureRuntimeCancelled
	} else if err != nil {
		outcome = executionmodel.ExecutionFailed
		if failureCode == "" {
			failureCode = contracts.ExecutionFailureRuntimeFailed
		}
	}
	return normalizeOutcome(outcome, failureCode)
}

func (r *Runtime) settleExecution(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	store runtimecontract.TranscriptStore,
	active *runtimeExecution,
	outcome executionmodel.ExecutionOutcome,
	failureCode contracts.ExecutionFailureCode,
) bool {
	settlementContext, cancelSettlement := r.settlementContext()
	defer cancelSettlement()
	settlement, err := store.AppendExecutionSettlement(settlementContext, runtimecontract.ExecutionSettlementReceipt{
		ExecutionID: execution.ID, RequestID: execution.RequestID,
		Outcome:     outcome,
		FailureCode: failureCode,
	})
	if err != nil {
		r.logError(execution, "append execution settlement", err)
		return false
	}
	if err := active.lifecycle.SettleRuntimeExecution(
		settlementContext,
		settlement.ExecutionID,
		settlement.Outcome,
		settlement.FailureCode,
	); err != nil {
		r.logError(execution, "settle product execution", err)
		return false
	}
	if r.eventObserver != nil {
		r.eventObserver(AgentEvent{
			Kind: AgentEventExecutionSettled, AgentID: execution.AgentID, ExecutionID: execution.ID,
		})
	}
	return true
}

func normalizeOutcome(
	outcome executionmodel.ExecutionOutcome,
	failureCode contracts.ExecutionFailureCode,
) (executionmodel.ExecutionOutcome, contracts.ExecutionFailureCode) {
	if !knownOutcome(outcome) {
		outcome = executionmodel.ExecutionFailed
		failureCode = contracts.ExecutionFailureRuntimeInvalid
	}
	if outcome == executionmodel.ExecutionFailed && failureCode == "" {
		failureCode = contracts.ExecutionFailureRuntimeFailed
	}
	if !failureCode.Valid() {
		outcome = executionmodel.ExecutionFailed
		failureCode = contracts.ExecutionFailureRuntimeFailed
	}
	return outcome, failureCode
}

func (r *Runtime) logError(execution executionmodel.AgentExecution, stage string, err error) {
	if err == nil {
		return
	}
	r.logger(execution, stage, err)
}

func (r *Runtime) logEvent(execution executionmodel.AgentExecution, stage string) {
	r.eventLogger(execution, stage)
}

func (r *Runtime) logEventLocked(executionID contracts.AgentExecutionID, stage string) {
	r.eventLogger(executionmodel.AgentExecution{ID: executionID, AgentID: r.agentID}, stage)
}

func defaultExecutionLogger(execution executionmodel.AgentExecution, stage string, err error) {
	if err == nil {
		return
	}
	log.Printf("praxis execution %s %s failed: %v", execution.ID, stage, err)
}

func (r *Runtime) settlementContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.settlementTimeout)
}

func knownOutcome(outcome executionmodel.ExecutionOutcome) bool {
	switch outcome {
	case executionmodel.ExecutionCompleted, executionmodel.ExecutionYielded, executionmodel.ExecutionPaused,
		executionmodel.ExecutionFailed, executionmodel.ExecutionInterrupted:
		return true
	default:
		return false
	}
}
