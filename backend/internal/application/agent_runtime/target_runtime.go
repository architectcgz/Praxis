package agentruntime

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	domainexecution "praxis/internal/core/domain/execution"
	domainfoundation "praxis/internal/core/domain/foundation"

	coreruntime "praxis/internal/core/runtime"
	coresession "praxis/internal/core/session"
)

// TargetSessionResolver returns the only JSONL writer for one Agent. The
// runtime cannot inspect other Agents' transcripts through this interface.
type TargetSessionResolver func(domainfoundation.SessionID, domainfoundation.AgentID) (coresession.TranscriptReceiptStore, error)

type TargetSessionHeaderResolver func(
	context.Context,
	domainexecution.AgentExecution,
) (coresession.AgentSessionHeader, error)

// TargetExecutionRunner is the provider/tool boundary. It receives a durable
// immutable execution snapshot and the Agent-owned transcript port, then
// returns only a stable settlement outcome.
type TargetExecutionRunner interface {
	RunWithSession(
		context.Context,
		domainexecution.AgentExecution,
		coresession.TranscriptReceiptStore,
	) (domainexecution.ExecutionOutcome, domainexecution.ExecutionFailureCode, error)
}

// TargetExecutionLogger receives sanitized runtime failures for diagnostics.
// It must not be used for provider payloads or credentials.
type TargetExecutionLogger func(domainexecution.AgentExecution, string, error)

// TargetExecutionEventLogger receives sanitized runtime lifecycle events.
type TargetExecutionEventLogger func(domainexecution.AgentExecution, string)

type TargetRuntimeConfig struct {
	AgentID           domainfoundation.AgentID
	Sessions          TargetSessionResolver
	Header            TargetSessionHeaderResolver
	Runner            TargetExecutionRunner
	Logger            TargetExecutionLogger
	EventLogger       TargetExecutionEventLogger
	OutputObserver    AgentOutputObserver
	SettlementTimeout time.Duration
}

// TargetRuntime is the long-lived process-local actor for one Agent. Its
// active execution state is deliberately ephemeral; recovery uses receipts.
type TargetRuntime struct {
	agentID           domainfoundation.AgentID
	sessions          TargetSessionResolver
	header            TargetSessionHeaderResolver
	runner            TargetExecutionRunner
	logger            TargetExecutionLogger
	eventLogger       TargetExecutionEventLogger
	outputObserver    AgentOutputObserver
	settlementTimeout time.Duration

	mu     sync.Mutex
	active *targetRuntimeExecution
	closed bool
}

type targetRuntimeExecution struct {
	id            domainfoundation.AgentExecutionID
	cancel        context.CancelFunc
	done          chan struct{}
	cancelOutcome domainexecution.ExecutionOutcome
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
		eventLogger = func(domainexecution.AgentExecution, string) {}
	}
	return &TargetRuntime{
		agentID:           config.AgentID,
		sessions:          config.Sessions,
		header:            config.Header,
		runner:            config.Runner,
		logger:            logger,
		eventLogger:       eventLogger,
		outputObserver:    config.OutputObserver,
		settlementTimeout: timeout,
	}, nil
}

// Activate accepts only a durable execution for this Agent. lifecycle belongs
// to that execution's receipt handoff; a duplicate activation is harmless, but
// a second active execution is not.
func (r *TargetRuntime) Activate(
	ctx context.Context,
	execution domainexecution.AgentExecution,
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
		cancelOutcome: domainexecution.ExecutionInterrupted,
		lifecycle:     lifecycle,
	}
	r.active = active
	go r.run(executionCtx, execution, active)
	r.logEvent(execution, "activation accepted")
	return nil
}

func (r *TargetRuntime) Cancel(
	ctx context.Context,
	executionID domainfoundation.AgentExecutionID,
	outcome domainexecution.ExecutionOutcome,
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
		active.cancelOutcome = domainexecution.ExecutionInterrupted
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
	execution domainexecution.AgentExecution,
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
	outcome, failureCode, err := r.runner.RunWithSession(ctx, execution, store)
	r.logEvent(execution, "provider completed")
	if err != nil {
		r.logError(execution, "run provider", err)
	}
	if ctx.Err() != nil {
		r.mu.Lock()
		outcome = active.cancelOutcome
		r.mu.Unlock()
		failureCode = domainexecution.ExecutionFailureRuntimeCancelled
	} else if err != nil {
		outcome = domainexecution.ExecutionFailed
		if failureCode == "" {
			failureCode = domainexecution.ExecutionFailureRuntimeFailed
		}
	}
	if !knownTargetOutcome(outcome) {
		outcome = domainexecution.ExecutionFailed
		failureCode = domainexecution.ExecutionFailureRuntimeInvalid
	}
	if outcome == domainexecution.ExecutionFailed && failureCode == "" {
		failureCode = domainexecution.ExecutionFailureRuntimeFailed
	}
	if !failureCode.Valid() {
		outcome = domainexecution.ExecutionFailed
		failureCode = domainexecution.ExecutionFailureRuntimeFailed
	}
	settlementContext, cancelSettlement := r.settlementContext()
	defer cancelSettlement()
	settlement, err := store.AppendExecutionSettlement(settlementContext, coresession.ExecutionSettlementReceipt{
		ExecutionID: execution.ID, RequestID: execution.RequestID,
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
		return
	}
	if r.outputObserver != nil {
		r.outputObserver(AgentOutputEvent{
			Kind: AgentOutputSettled, AgentID: execution.AgentID, ExecutionID: execution.ID,
		})
	}
	r.logEvent(execution, "execution settled")
}

func (r *TargetRuntime) logError(execution domainexecution.AgentExecution, stage string, err error) {
	if err == nil {
		return
	}
	r.logger(execution, stage, err)
}

func (r *TargetRuntime) logEvent(execution domainexecution.AgentExecution, stage string) {
	r.eventLogger(execution, stage)
}

func (r *TargetRuntime) logEventLocked(executionID domainfoundation.AgentExecutionID, stage string) {
	r.eventLogger(domainexecution.AgentExecution{ID: executionID, AgentID: r.agentID}, stage)
}

func defaultTargetExecutionLogger(execution domainexecution.AgentExecution, stage string, err error) {
	if err == nil {
		return
	}
	log.Printf("praxis execution %s %s failed: %v", execution.ID, stage, err)
}

func (r *TargetRuntime) settlementContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), r.settlementTimeout)
}

func knownTargetOutcome(outcome domainexecution.ExecutionOutcome) bool {
	switch outcome {
	case domainexecution.ExecutionCompleted, domainexecution.ExecutionYielded, domainexecution.ExecutionPaused,
		domainexecution.ExecutionFailed, domainexecution.ExecutionInterrupted:
		return true
	default:
		return false
	}
}
