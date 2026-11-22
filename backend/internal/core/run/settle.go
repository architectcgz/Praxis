package run

import (
	"context"
	"errors"
	"fmt"
	agentmodel "praxis/internal/core/agent"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
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
		EndedAt:          time.Now().UTC(),
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
		ID:        identity.NewEntryID(),
		RunID:     r.id,
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
	if errors.Is(context.Cause(ctx), errorcode.ErrStorage) {
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
	if errors.Is(err, errorcode.ErrStorage) {
		return sessionentry.RunFailureStorage
	}
	if _, ok := errors.AsType[*appcontext.ContextWindowExceededError](err); ok {
		return sessionentry.RunFailureResourceLimit
	}
	if errors.Is(err, errorcode.ErrProvider) {
		return sessionentry.RunFailureProvider
	}
	if errors.Is(err, errorcode.ErrContract) {
		return sessionentry.RunFailureContract
	}
	return sessionentry.RunFailureInternal
}

// closeToolCalls 在失败收尾前补齐已接纳声明的回执，不重新执行工具。
// 日志尚未确认完整时 AppendEntry 直接返回保存错误，此时不能发布 Run 终态。
func (r *Run) closeToolCalls(ctx context.Context) error {
	for _, receipt := range sessionentry.MissingToolResultEntries(r.entries, time.Now().UTC()) {
		stored, _, err := r.AppendEntry(ctx, receipt)
		if err != nil {
			return fmt.Errorf("close tool receipts: %w", err)
		}
		r.deps.Emitter.Emit(coreevent.Event{
			Source:  r.EventSource(receipt.TurnID),
			Payload: coreevent.EntriesAppended{Entries: []*sessionentry.Entry{stored}},
		})
	}
	return nil
}
