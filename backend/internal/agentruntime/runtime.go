package agentruntime

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"praxis/internal/core/domain"
)

// Runtime owns one AgentThread's model/tool lifecycle and never owns product approval state.
type Runtime struct {
	config RuntimeConfig
	guard  *PhaseGuard

	mu             sync.Mutex
	queueMu        sync.Mutex
	entryMu        sync.Mutex
	eventMu        sync.Mutex
	activeRun      *domain.AgentRun
	activeContext  context.Context
	activeCancel   context.CancelFunc
	settlementBase context.Context
	done           chan struct{}
	lastError      error
	abortRequested bool
	closing        bool
	closed         bool
	turnCount      int

	queues        runtimeQueues
	eventSequence uint64
	events        chan RuntimeEvent
	lifecycleDone chan struct{}
}

// New validates and freezes a runtime's capability and execution inputs.
func New(config RuntimeConfig) (*Runtime, error) {
	normalized, err := normalizeConfig(config)
	if err != nil {
		return nil, err
	}
	buffer := normalized.EventBuffer
	if buffer == 0 {
		buffer = 128
	}
	return &Runtime{
		config:        normalized,
		guard:         NewPhaseGuard(),
		events:        make(chan RuntimeEvent, buffer),
		lifecycleDone: make(chan struct{}),
	}, nil
}

// Events returns the ordered runtime event stream.
func (r *Runtime) Events() <-chan RuntimeEvent { return r.events }

// Phase returns the current structural operation phase.
func (r *Runtime) Phase() AgentRuntimePhase { return r.guard.Current() }

// StartExecution activates one validated run and starts its asynchronous turn
// loop. Product persistence is owned by the caller; the runtime only owns the
// session receipt and model/tool lifecycle after this boundary accepts the run.
// onSettle belongs to this run and is not retained by the reusable runtime.
func (r *Runtime) StartExecution(
	ctx context.Context,
	run domain.AgentRun,
	prompt string,
	onSettle SettlementHandler,
) error {
	if ctx == nil {
		return &RuntimeError{Code: ErrorContract, Message: "start context is required"}
	}
	if err := r.guard.Acquire(PhaseTurn); err != nil {
		return err
	}
	normalizedRun := run.Snapshot()
	if err := normalizedRun.Validate(); err != nil {
		r.guard.Release()
		return &RuntimeError{Code: ErrorContract, Message: "run is invalid", Cause: err}
	}
	if err := validateRunForRuntime(normalizedRun, r.config.AgentThreadID); err != nil {
		r.guard.Release()
		return err
	}
	if normalizedRun.Execution != r.config.Execution {
		r.guard.Release()
		return &RuntimeError{Code: ErrorContract, Message: "run execution snapshot does not match runtime config"}
	}

	r.mu.Lock()
	if r.closed || r.closing {
		r.mu.Unlock()
		r.guard.Release()
		return &RuntimeError{Code: ErrorClosed, Message: "runtime is closed"}
	}
	if r.activeRun != nil {
		r.mu.Unlock()
		r.guard.Release()
		return &RuntimeError{Code: ErrorBusy, Message: "runtime already has an active run"}
	}
	r.activeRun = &normalizedRun
	r.activeContext, r.activeCancel = context.WithCancel(ctx)
	r.settlementBase = context.WithoutCancel(ctx)
	r.done = make(chan struct{})
	r.lastError = nil
	r.abortRequested = false
	r.turnCount = 0
	r.mu.Unlock()

	if err := r.initializeSession(ctx, normalizedRun, prompt); err != nil {
		r.abortStart(err)
		return err
	}
	go r.executeRun(onSettle)
	return nil
}

func (r *Runtime) initializeSession(ctx context.Context, run domain.AgentRun, prompt string) error {
	projection, err := r.config.SessionStore.ReadContext(ctx, r.config.SessionReference)
	if err != nil {
		return &RuntimeError{Code: ErrorStorage, Message: "session context could not be initialized", Cause: err}
	}
	if projection.HasManifest && !projection.CanContinue {
		return &RuntimeError{Code: ErrorContract, Message: "session requires a newer reader version"}
	}
	if !projection.HasManifest {
		if _, err := r.config.SessionStore.Initialize(ctx, r.sessionManifest(), r.now()); err != nil {
			return &RuntimeError{Code: ErrorStorage, Message: "session manifest could not be initialized", Cause: err}
		}
	}
	if _, err := r.appendSessionEvent(ctx, SessionEventRunStarted, RunStartedEvent{
		Reason: run.Reason,
		Inputs: r.runInputs(run),
	}, true); err != nil {
		return err
	}
	if strings.TrimSpace(prompt) != "" {
		if _, err := r.appendSessionEvent(ctx, SessionEventMessage, MessageEvent{Message: TurnMessage{
			Role:    TurnRoleUser,
			Content: []TurnContentBlock{{Kind: TurnContentText, Text: strings.TrimSpace(prompt)}},
		}}, true); err != nil {
			return err
		}
	}
	return r.drainNextTurn(ctx)
}

func (r *Runtime) abortStart(err error) {
	r.mu.Lock()
	if r.activeCancel != nil {
		r.activeCancel()
	}
	r.activeRun = nil
	r.activeContext = nil
	r.activeCancel = nil
	r.lastError = err
	if r.done != nil {
		close(r.done)
		r.done = nil
	}
	r.mu.Unlock()
	r.guard.Release()
}

// RequestPause cancels the active run and waits for durable settlement.
func (r *Runtime) RequestPause(ctx context.Context) error {
	if ctx == nil {
		return &RuntimeError{Code: ErrorContract, Message: "pause context is required"}
	}
	r.mu.Lock()
	if r.activeRun == nil {
		r.mu.Unlock()
		return &RuntimeError{Code: ErrorBusy, Message: "runtime is not running"}
	}
	r.abortRequested = true
	if err := r.guard.Stop(); err != nil {
		r.mu.Unlock()
		return err
	}
	cancel := r.activeCancel
	done := r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	select {
	case <-done:
		r.mu.Lock()
		err := r.lastError
		r.mu.Unlock()
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// RequestAbort is the explicit abort spelling used by runtime callers.
func (r *Runtime) RequestAbort(ctx context.Context) error { return r.RequestPause(ctx) }

// Close stops the runtime, waits for an active run, and closes its event stream.
func (r *Runtime) Close(ctx context.Context) error {
	if ctx == nil {
		return &RuntimeError{Code: ErrorContract, Message: "close context is required"}
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closing = true
	active := r.activeRun != nil
	r.mu.Unlock()
	var err error
	if active {
		err = r.RequestPause(ctx)
	}
	r.mu.Lock()
	if r.activeRun != nil {
		r.mu.Unlock()
		return errOr(err, &RuntimeError{Code: ErrorInterrupted, Message: "runtime did not settle before close"})
	}
	r.closed = true
	close(r.lifecycleDone)
	close(r.events)
	r.mu.Unlock()
	return err
}

func errOr(first, fallback error) error {
	if first != nil {
		return first
	}
	return fallback
}

func (r *Runtime) executeRun(onSettle SettlementHandler) {
	defer func() {
		if recovered := recover(); recovered != nil {
			_ = r.settle(
				context.WithoutCancel(r.settlementContext()),
				domain.RunFailed,
				"runtime_panic",
				onSettle,
			)
		}
	}()
	ctx := r.runningContext()
	result := r.runLoop(ctx)
	if err := r.settle(ctx, result.outcome, result.failureCode, onSettle); err != nil {
		r.mu.Lock()
		r.lastError = err
		r.mu.Unlock()
	}
}

func (r *Runtime) runningContext() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.activeContext
}

func (r *Runtime) settlementContext() context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.settlementBase != nil {
		return r.settlementBase
	}
	return context.Background()
}

func (r *Runtime) currentRunState() (domain.AgentRun, int, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.activeRun == nil {
		return domain.AgentRun{}, 0, false, fmt.Errorf("runtime has no active run")
	}
	return r.activeRun.Snapshot(), r.turnCount, r.abortRequested, nil
}
