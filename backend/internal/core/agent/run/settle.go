package run

import (
	"context"
	"errors"
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"strings"
	"time"
)

// SettlePreparation 结束已经写入 RunPrepared、但没有进入 Loop 的执行。
func (r *Run) SettlePreparation(ctx context.Context, err error) error {
	outcome, code, message := r.classifyOutcome(ctx, err)
	endCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return r.Settle(endCtx, outcome, code, message)
}

// settleExecution 用原执行上下文区分中断和失败；回执和终态的写入不继承执行取消信号。
func (r *Run) settleExecution(ctx context.Context, execErr error) error {
	outcome, code, message := r.classifyOutcome(ctx, execErr)
	if execErr != nil && outcome == sessionentry.OutcomeFailed {
		r.emitError(r.currentTurnID, execErr)
	}
	endCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	if err := r.closeToolCalls(endCtx); err != nil {
		return err
	}
	return r.Settle(endCtx, outcome, code, message)
}

func (r *Run) Settle(ctx context.Context, outcome sessionentry.RunOutcome, code sessionentry.RunFailureCode, message string) error {
	stats := sessionentry.RunStats{
		StartedAt:        r.startedAt,
		EndedAt:          r.deps.Clock.Now(),
		Usage:            r.progress.Usage,
		ModelRequests:    r.progress.ModelRequests,
		Iterations:       r.progress.Iterations,
		OutputBytes:      r.progress.OutputBytes,
		ToolCalls:        r.progress.ToolCalls,
		FinalAssistantID: r.progress.FinalAssistantID,
		FinalStopReason:  r.progress.FinalStopReason,
	}
	var payload sessionentry.Payload
	state := agentmodel.AgentIdle
	switch outcome {
	case sessionentry.OutcomeCompleted:
		payload = sessionentry.RunCompleted{RunStats: stats}
	case sessionentry.OutcomeYielded:
		payload = sessionentry.SubagentYielded{RunStats: stats}
	case sessionentry.OutcomeFailed:
		state = agentmodel.AgentFailed
		payload = sessionentry.RunFailed{
			RunStats: stats,
			Code:     code,
			Message:  message,
		}
	case sessionentry.OutcomeInterrupted:
		state = agentmodel.AgentInterrupted
		payload = sessionentry.RunInterrupted{
			RunStats: stats,
			Reason:   string(code),
		}
	}
	stored, err := r.history.CommitTerminal(ctx, sessionentry.Entry{
		ID:        "run:" + r.id.String(),
		RunID:     r.id.String(),
		CreatedAt: stats.EndedAt,
		Payload:   payload,
	})
	if err != nil {
		return storageFailure(err)
	}
	r.terminalMu.Lock()
	r.terminal = stored
	r.terminalMu.Unlock()
	r.accept(stored)
	if hookErr := r.deps.HookDispatcher.RunCommitted(context.WithoutCancel(ctx), hook.RunEvent{
		Identity:    runIdentity(r, ""),
		Outcome:     string(outcome),
		FailureCode: string(code),
	}); hookErr != nil {
		r.deps.Logger.Errorf("run committed hook failed: %v", hookErr)
	}
	r.deps.Emitter.Emit(coreevent.Event{
		Source: r.EventSource(""),
		Payload: coreevent.RunEnded{
			StartedAt:      stats.StartedAt,
			EndedAt:        stats.EndedAt,
			Outcome:        outcome,
			Iterations:     stats.Iterations,
			ToolCalls:      stats.ToolCalls,
			FailureCode:    code,
			FailureMessage: message,
			State:          state,
		},
	})
	return nil
}

func (r *Run) classifyOutcome(ctx context.Context, err error) (sessionentry.RunOutcome, sessionentry.RunFailureCode, string) {
	if loop.FailureClassification(context.Cause(ctx)) == loop.FailureStorage {
		return sessionentry.OutcomeFailed, sessionentry.RunFailureStorage, ""
	}
	if err == nil {
		if !r.Main() {
			return sessionentry.OutcomeYielded, "", ""
		}
		return sessionentry.OutcomeCompleted, "", ""
	}
	code := failureCode(err)
	if errors.Is(context.Cause(ctx), context.DeadlineExceeded) {
		return sessionentry.OutcomeFailed, sessionentry.RunFailureTimeout, ""
	}
	if code != sessionentry.RunFailureStorage && ctx.Err() != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, context.Cause(ctx))) {
		code = sessionentry.RunFailureCancelled
		if errors.Is(context.Cause(ctx), ErrInterrupted) {
			code = sessionentry.RunFailureRequestCanceled
		}
		return sessionentry.OutcomeInterrupted, code, ""
	}
	message := ""
	if code == sessionentry.RunFailureProvider {
		text := []rune(strings.TrimSpace(err.Error()))
		if len(text) > sessionentry.MaxRunFailureMessageRunes {
			text = text[:sessionentry.MaxRunFailureMessageRunes]
		}
		message = string(text)
	}
	return sessionentry.OutcomeFailed, code, message
}

func failureCode(err error) sessionentry.RunFailureCode {
	switch loop.FailureClassification(err) {
	case loop.FailureProviderUnavailable:
		return sessionentry.RunFailureProviderUnavailable
	case loop.FailureProvider:
		return sessionentry.RunFailureProvider
	case loop.FailureTool:
		return sessionentry.RunFailureTool
	case loop.FailurePolicyBlocked:
		return sessionentry.RunFailurePolicyBlocked
	case loop.FailureResourceLimit:
		return sessionentry.RunFailureResourceLimit
	case loop.FailureStorage:
		return sessionentry.RunFailureStorage
	case loop.FailureContract:
		return sessionentry.RunFailureContract
	case loop.FailureBusy:
		return sessionentry.RunFailureBusy
	default:
		return sessionentry.RunFailureInternal
	}
}
