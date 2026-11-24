package run

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/agent"
	appcontext "praxis/internal/context"
	"praxis/internal/engine/hook"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"time"
)

// SettlePreparation 结束已经写入 RunPrepared、但没有进入 Loop 的执行。
func (r *Run) SettlePreparation(ctx context.Context, err error) error {
	outcome, code, message := r.classifyOutcome(ctx, err)
	return r.Settle(ctx, outcome, code, message)
}

// settleExecution 用原执行上下文区分中断和失败；回执和终态的写入不继承执行取消信号。
func (r *Run) settleExecution(ctx context.Context, execErr error) error {
	outcome, code, message := r.classifyOutcome(ctx, execErr)
	if execErr != nil && outcome == entry.OutcomeFailed {
		r.emitError(r.currentTurnID, code, message)
	}
	endCtx, cancel := r.settlementContext(ctx)
	defer cancel()
	if err := r.closeToolCalls(endCtx); err != nil {
		return err
	}
	return r.settle(endCtx, outcome, code, message)
}

func (r *Run) settlementContext(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), r.deps.SettleTimeout)
}

// Settle 保存未能启动的 Run 的终态，不继承启动路径的取消信号。
func (r *Run) Settle(ctx context.Context, outcome entry.RunOutcome, code entry.RunFailureCode, message string) error {
	endCtx, cancel := r.settlementContext(ctx)
	defer cancel()
	return r.settle(endCtx, outcome, code, message)
}

func (r *Run) settle(ctx context.Context, outcome entry.RunOutcome, code entry.RunFailureCode, message string) error {
	stats := entry.RunStats{
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
	var payload entry.Payload
	state := agent.AgentIdle
	switch outcome {
	case entry.OutcomeCompleted:
		payload = entry.RunCompleted{RunStats: stats}
	case entry.OutcomeYielded:
		payload = entry.SubagentYielded{RunStats: stats}
	case entry.OutcomeFailed:
		state = agent.AgentFailed
		payload = entry.RunFailed{
			RunStats: stats,
			Code:     code,
			Message:  message,
		}
	case entry.OutcomeInterrupted:
		state = agent.AgentInterrupted
		payload = entry.RunInterrupted{
			RunStats: stats,
			Reason:   string(code),
		}
	}
	stored, err := r.history.CommitTerminal(ctx, entry.Entry{
		ID:        identity.NewEntryID(),
		RunID:     r.id,
		CreatedAt: stats.EndedAt,
		Payload:   payload,
	})
	if err != nil {
		return storageFailure(err)
	}
	r.terminal = stored
	r.terminalSaved.Store(true)
	r.accept(stored)
	if hookErr := r.deps.HookDispatcher.RunCommitted(ctx, hook.RunEvent{
		Identity:    runIdentity(r, ""),
		Outcome:     string(outcome),
		FailureCode: string(code),
	}); hookErr != nil {
		r.deps.Logger.Errorf("run committed hook failed: %v", hookErr)
	}
	r.deps.Emitter.Emit(event.Event{
		Source: r.EventSource(""),
		Payload: event.RunEnded{
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

func (r *Run) classifyOutcome(ctx context.Context, err error) (entry.RunOutcome, entry.RunFailureCode, string) {
	cause := context.Cause(ctx)
	if errors.Is(cause, errorcode.ErrStorage) {
		return entry.OutcomeFailed, entry.RunFailureStorage, ""
	}
	if err == nil {
		if !r.Main() {
			return entry.OutcomeYielded, "", ""
		}
		return entry.OutcomeCompleted, "", ""
	}
	code := failureCode(err)
	if code != entry.RunFailureStorage && (errors.Is(cause, context.DeadlineExceeded) ||
		(code != entry.RunFailureProvider && errors.Is(err, context.DeadlineExceeded))) {
		return entry.OutcomeFailed, entry.RunFailureTimeout, ""
	}
	if code != entry.RunFailureStorage && ctx.Err() != nil && (errors.Is(err, ctx.Err()) || errors.Is(err, cause)) {
		code = entry.RunFailureCancelled
		if errors.Is(cause, ErrInterrupted) {
			code = entry.RunFailureRequestCanceled
		}
		return entry.OutcomeInterrupted, code, ""
	}
	message := ""
	if code == entry.RunFailureProvider {
		message = entry.FormatRunFailureMessage(err.Error())
	}
	return entry.OutcomeFailed, code, message
}

func failureCode(err error) entry.RunFailureCode {
	if errors.Is(err, errorcode.ErrStorage) {
		return entry.RunFailureStorage
	}
	if _, ok := errors.AsType[*appcontext.ContextWindowExceededError](err); ok {
		return entry.RunFailureResourceLimit
	}
	if errors.Is(err, errorcode.ErrProvider) {
		return entry.RunFailureProvider
	}
	if errors.Is(err, errorcode.ErrContract) {
		return entry.RunFailureContract
	}
	return entry.RunFailureInternal
}

// closeToolCalls 在失败收尾前补齐已接纳声明的回执，不重新执行工具。
// 日志尚未确认完整时 AppendEntry 直接返回保存错误，此时不能发布 Run 终态。
func (r *Run) closeToolCalls(ctx context.Context) error {
	for _, receipt := range entry.MissingToolResultEntries(r.entries, time.Now().UTC()) {
		stored, _, err := r.AppendEntry(ctx, receipt)
		if err != nil {
			return fmt.Errorf("close tool receipts: %w", err)
		}
		r.deps.Emitter.Emit(event.Event{
			Source:  r.EventSource(receipt.TurnID),
			Payload: event.EntriesAppended{Entries: []*entry.Entry{stored}},
		})
	}
	return nil
}
