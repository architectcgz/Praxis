package agentruntime

import (
	"context"
	"strconv"

	"praxis/internal/core/domain"
)

func (r *Runtime) settle(
	ctx context.Context,
	outcome domain.AgentRunOutcome,
	failureCode string,
	onSettle SettlementHandler,
) error {
	base := r.settlementContext()
	if ctx == nil || ctx.Err() != nil {
		ctx = base
	}
	r.mu.Lock()
	if r.activeRun == nil {
		r.mu.Unlock()
		return nil
	}
	run := r.activeRun.Snapshot()
	turnCount := r.turnCount
	r.mu.Unlock()
	defer r.guard.Release()
	settlement := Settlement{
		RunID:       run.ID,
		ThreadID:    run.AgentThreadID,
		Outcome:     outcome,
		FailureCode: failureCode,
		TurnCount:   turnCount,
	}

	if err := r.flush(ctx); err != nil {
		settlement.Outcome = domain.RunFailed
		settlement.FailureCode = string(ErrorStorage)
		failureCode = settlement.FailureCode
	}
	if outcome == domain.RunInterrupted {
		if _, err := r.appendSessionEvent(ctx, SessionEventInterrupted, OperationInterruptedEvent{
			Operation: "run",
			TargetID:  run.ID.String(),
			Note:      string(ErrorInterrupted),
		}, true); err != nil {
			settlement.Outcome = domain.RunFailed
			settlement.FailureCode = string(ErrorStorage)
			failureCode = settlement.FailureCode
		}
	}
	for _, listener := range r.config.Critical {
		if listener == nil {
			continue
		}
		if err := listener(ctx, settlement); err != nil {
			settlement.Outcome = domain.RunFailed
			settlement.FailureCode = string(ErrorStorage)
			failureCode = settlement.FailureCode
			break
		}
	}
	// The transcript receipt is the recovery source of truth. Product state must
	// not settle until the terminal outcome is durably visible there.
	entry, err := r.appendSessionEvent(ctx, SessionEventRunSettled, RunSettledEvent{
		Outcome:    settlement.Outcome,
		ErrorClass: failureCode,
		TurnCount:  settlement.TurnCount,
	}, false)
	if err != nil {
		r.finish(err)
		return err
	}
	if err := r.flush(ctx); err != nil {
		r.finish(err)
		return err
	}
	if onSettle != nil {
		if err := onSettle(ctx, settlement); err != nil {
			r.finish(err)
			return &RuntimeError{Code: ErrorStorage, Message: "product settlement failed", Cause: err}
		}
	}
	r.publish(RuntimeEvent{
		TaskSessionID: r.config.TaskSessionID,
		AgentThreadID: r.config.AgentThreadID,
		AgentRunID:    run.ID,
		Sequence:      r.nextEventSequence(),
		Kind:          RuntimeEventRunSettled,
		Payload: map[string]string{
			"outcome":       string(settlement.Outcome),
			"entrySequence": strconv.FormatUint(entry.Sequence, 10),
		},
	})
	r.finish(nil)
	return nil
}

func (r *Runtime) finish(err error) {
	r.mu.Lock()
	if err != nil {
		r.lastError = err
	}
	if r.activeCancel != nil {
		r.activeCancel()
	}
	r.activeRun = nil
	r.activeContext = nil
	r.activeCancel = nil
	done := r.done
	r.done = nil
	r.mu.Unlock()
	if done != nil {
		close(done)
	}
}

// LastError returns the most recent runtime or settlement error.
func (r *Runtime) LastError() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastError
}
