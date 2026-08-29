package agentruntime

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"praxis/internal/core/domain"
	coreruntime "praxis/internal/core/runtime"
	coresession "praxis/internal/core/session"
)

// TargetSessionResolver returns the only JSONL writer for one Agent. The
// runtime cannot inspect other Agents' transcripts through this interface.
type TargetSessionResolver func(domain.SessionID, domain.AgentID) (coresession.TranscriptReceiptStore, error)

type TargetSessionHeaderResolver func(
	context.Context,
	domain.AgentExecution,
) (coresession.AgentSessionHeader, error)

// TargetExecutionRunner is the provider/tool boundary. It receives a durable
// immutable execution snapshot and returns only a stable settlement outcome.
type TargetExecutionRunner interface {
	Run(context.Context, domain.AgentExecution) (domain.ExecutionOutcome, domain.ExecutionFailureCode, error)
}

// TargetExecutionSessionRunner receives the already-open Agent transcript so
// provider output and runtime receipts share one sequence allocator.
type TargetExecutionSessionRunner interface {
	RunWithSession(
		context.Context,
		domain.AgentExecution,
		coresession.TranscriptReceiptStore,
	) (domain.ExecutionOutcome, domain.ExecutionFailureCode, error)
}

// TargetExecutionLogger receives sanitized runtime failures for diagnostics.
// It must not be used for provider payloads or credentials.
type TargetExecutionLogger func(domain.AgentExecution, string, error)

// TargetExecutionEventLogger receives sanitized runtime lifecycle events.
type TargetExecutionEventLogger func(domain.AgentExecution, string)

type TargetRuntimeConfig struct {
	AgentID           domain.AgentID
	Sessions          TargetSessionResolver
	Header            TargetSessionHeaderResolver
	Runner            TargetExecutionRunner
	Logger            TargetExecutionLogger
	EventLogger       TargetExecutionEventLogger
	SettlementTimeout time.Duration
}

// TargetRuntime is the long-lived process-local actor for one Agent. Its
// active execution state is deliberately ephemeral; recovery uses receipts.
type TargetRuntime struct {
	agentID           domain.AgentID
	sessions          TargetSessionResolver
	header            TargetSessionHeaderResolver
	runner            TargetExecutionRunner
	logger            TargetExecutionLogger
	eventLogger       TargetExecutionEventLogger
	settlementTimeout time.Duration

	mu     sync.Mutex
	active *targetRuntimeExecution
	closed bool
}

type targetRuntimeExecution struct {
	id            domain.AgentExecutionID
	cancel        context.CancelFunc
	done          chan struct{}
	cancelOutcome domain.ExecutionOutcome
	lifecycle     coreruntime.ExecutionLifecycle
}

func NewTargetRuntime(config TargetRuntimeConfig) (*TargetRuntime, error) {
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
		logger = defaultTargetExecutionLogger
	}
	eventLogger := config.EventLogger
	if eventLogger == nil {
		eventLogger = func(domain.AgentExecution, string) {}
	}
	return &TargetRuntime{
		agentID:           config.AgentID,
		sessions:          config.Sessions,
		header:            config.Header,
		runner:            config.Runner,
		logger:            logger,
		eventLogger:       eventLogger,
		settlementTimeout: timeout,
	}, nil
}

// Activate accepts only a durable execution for this Agent. lifecycle belongs
// to that execution's receipt handoff; a duplicate activation is harmless, but
// a second active execution is not.
func (r *TargetRuntime) Activate(
	ctx context.Context,
	execution domain.AgentExecution,
	lifecycle coreruntime.ExecutionLifecycle,
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
	active := &targetRuntimeExecution{
		id:            execution.ID,
		cancel:        cancel,
		done:          make(chan struct{}),
		cancelOutcome: domain.ExecutionInterrupted,
		lifecycle:     lifecycle,
	}
	r.active = active
	go r.run(executionCtx, execution, active)
	r.logEvent(execution, "activation accepted")
	return nil
}

func (r *TargetRuntime) Cancel(
	ctx context.Context,
	executionID domain.AgentExecutionID,
	outcome domain.ExecutionOutcome,
) error {
	if ctx == nil {
		return errors.New("target runtime cancellation context is required")
	}
	if !knownTargetOutcome(outcome) {
		return errors.New("target runtime cancellation outcome is invalid")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.active == nil || r.active.id != executionID {
		return nil
	}
	r.active.cancelOutcome = outcome
	r.active.cancel()
	r.logEventLocked(executionID, "cancellation requested")
	return nil
}

// Close cancels the active execution and waits only for its durable settlement
// up to the caller's deadline. A caller timeout never suppresses recovery.
func (r *TargetRuntime) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("target runtime close context is required")
	}
	r.mu.Lock()
	r.closed = true
	active := r.active
	if active != nil {
		active.cancelOutcome = domain.ExecutionInterrupted
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

func (r *TargetRuntime) run(
	ctx context.Context,
	execution domain.AgentExecution,
	active *targetRuntimeExecution,
) {
	r.logEvent(execution, "execution started")
	defer func() {
		r.mu.Lock()
		if r.active == active {
			r.active = nil
		}
		close(active.done)
		r.mu.Unlock()
	}()
	store, err := r.sessions(execution.SessionID, execution.AgentID)
	if err != nil || store == nil {
		if err == nil {
			err = errors.New("session resolver returned a nil store")
		}
		r.logError(execution, "open session", err)
		return
	}
	defer func() { _ = store.Close(context.Background()) }()
	if r.header != nil {
		header, err := r.header(ctx, execution)
		if err != nil {
			r.logError(execution, "resolve session header", err)
			return
		}
		if err := store.Initialize(ctx, header); err != nil {
			r.logError(execution, "initialize session", err)
			return
		}
	}
	start, err := store.AppendExecutionStart(ctx, execution)
	if err != nil {
		r.logError(execution, "append execution start", err)
		return
	}
	startContext, cancelStart := r.settlementContext()
	err = active.lifecycle.ConfirmExecutionStart(startContext, start)
	cancelStart()
	if err != nil {
		r.logError(execution, "confirm execution start", err)
		return
	}
	var outcome domain.ExecutionOutcome
	var failureCode domain.ExecutionFailureCode
	if sessionRunner, ok := r.runner.(TargetExecutionSessionRunner); ok {
		outcome, failureCode, err = sessionRunner.RunWithSession(ctx, execution, store)
	} else {
		outcome, failureCode, err = r.runner.Run(ctx, execution)
	}
	r.logEvent(execution, "provider completed")
	if err != nil {
		r.logError(execution, "run provider", err)
	}
	if ctx.Err() != nil {
		r.mu.Lock()
		outcome = active.cancelOutcome
		r.mu.Unlock()
		failureCode = domain.ExecutionFailureRuntimeCancelled
	} else if err != nil {
		outcome = domain.ExecutionFailed
		if failureCode == "" {
			failureCode = domain.ExecutionFailureRuntimeFailed
		}
	}
	if !knownTargetOutcome(outcome) {
		outcome = domain.ExecutionFailed
		failureCode = domain.ExecutionFailureRuntimeInvalid
	}
	if outcome == domain.ExecutionFailed && failureCode == "" {
		failureCode = domain.ExecutionFailureRuntimeFailed
	}
	if !failureCode.Valid() {
		outcome = domain.ExecutionFailed
		failureCode = domain.ExecutionFailureRuntimeFailed
	}
	settlementContext, cancelSettlement := r.settlementContext()
	defer cancelSettlement()
	settlement, err := store.AppendExecutionSettlement(settlementContext, coresession.ExecutionSettlementReceipt{
		ExecutionID: execution.ID,
		Outcome:     outcome,
		FailureCode: failureCode,
	})
	if err != nil {
		r.logError(execution, "append execution settlement", err)
		return
	}
	if err := active.lifecycle.SettleRuntimeExecution(
		settlementContext,
		settlement.ExecutionID,
		settlement.Outcome,
		settlement.FailureCode,
	); err != nil {
		r.logError(execution, "settle product execution", err)
	}
	r.logEvent(execution, "execution settled")
}

func (r *TargetRuntime) logError(execution domain.AgentExecution, stage string, err error) {
	if err == nil {
		return
	}
	r.logger(execution, stage, err)
}

func (r *TargetRuntime) logEvent(execution domain.AgentExecution, stage string) {
	r.eventLogger(execution, stage)
}

func (r *TargetRuntime) logEventLocked(executionID domain.AgentExecutionID, stage string) {
	r.eventLogger(domain.AgentExecution{ID: executionID, AgentID: r.agentID}, stage)
}

func defaultTargetExecutionLogger(execution domain.AgentExecution, stage string, err error) {
	if err == nil {
		return
	}
	log.Printf("praxis execution %s %s failed: %v", execution.ID, stage, err)
}

func (r *TargetRuntime) settlementContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.settlementTimeout)
}

func knownTargetOutcome(outcome domain.ExecutionOutcome) bool {
	switch outcome {
	case domain.ExecutionCompleted, domain.ExecutionYielded, domain.ExecutionPaused,
		domain.ExecutionFailed, domain.ExecutionInterrupted:
		return true
	default:
		return false
	}
}
