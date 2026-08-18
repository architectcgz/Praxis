package agentruntime

import (
	"context"
	"encoding/json"
	"fmt"

	"praxis/internal/core/domain"
)

func (r *Runtime) settle(ctx context.Context, outcome domain.AgentRunOutcome, failureCode string) error {
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
	settlement := Settlement{RunID: run.ID, ThreadID: run.AgentThreadID, Outcome: outcome, FailureCode: failureCode, TurnCount: turnCount}

	if err := r.flush(ctx); err != nil {
		settlement.Outcome = domain.RunFailed
		settlement.FailureCode = string(ErrorStorage)
		failureCode = settlement.FailureCode
	}
	if outcome == domain.RunInterrupted {
		payload, marshalErr := json.Marshal(map[string]string{"reason": string(ErrorInterrupted)})
		if marshalErr == nil {
			if err := r.appendEntry(ctx, EntryOperationInterrupted, payload, true); err != nil {
				settlement.Outcome = domain.RunFailed
				settlement.FailureCode = string(ErrorStorage)
				failureCode = settlement.FailureCode
			}
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
	r.guard.Release()
	if r.config.OnSettle != nil {
		if err := r.config.OnSettle(ctx, settlement); err != nil {
			r.finish(err)
			return &RuntimeError{Code: ErrorStorage, Message: "product settlement failed", Cause: err}
		}
	}
	settledPayload, err := json.Marshal(runSettledPayload{Outcome: string(settlement.Outcome), ErrorClass: failureCode, TurnCount: settlement.TurnCount})
	if err != nil {
		r.finish(err)
		return err
	}
	if err := r.appendEntry(ctx, EntryRunSettled, settledPayload, false); err != nil {
		r.finish(err)
		return err
	}
	entrySequence := r.currentEntrySequence()
	r.publish(RuntimeEvent{
		TaskSessionID: r.config.TaskSessionID,
		AgentThreadID: r.config.AgentThreadID,
		AgentRunID:    run.ID,
		Sequence:      r.nextEventSequence(),
		Kind:          RuntimeEventRunSettled,
		Payload: map[string]string{
			"outcome":       string(settlement.Outcome),
			"entrySequence": fmt.Sprintf("%d", entrySequence),
		},
	})
	r.finish(nil)
	return nil
}

func (r *Runtime) currentEntrySequence() uint64 {
	r.entryMu.Lock()
	defer r.entryMu.Unlock()
	return r.entrySequence
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
