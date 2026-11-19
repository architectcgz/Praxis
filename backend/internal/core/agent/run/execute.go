package run

import (
	"context"
	"errors"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/entry/role"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/errorcode"
	"strings"
	"time"
)

const ErrInterrupted errorcode.Code = "run.interrupted"

// Prepare 提交 Run 和输入事实。返回值表示 RunPrepared 已写入历史。
func (r *Run) Prepare(ctx context.Context, prompt string) (bool, error) {
	inputID := "input:" + r.id.String()
	authorKind := sessionentry.AuthorAgent
	if r.Main() {
		inputID = "input:" + r.sourceTaskID.String()
		authorKind = sessionentry.AuthorUser
	}
	if _, _, err := r.AppendEntry(ctx, sessionentry.Entry{
		ID:        "prepared:" + r.id.String(),
		CreatedAt: r.createdAt,
		Payload: sessionentry.RunPrepared{
			AgentName:    r.definition.Name,
			DefinitionID: r.definition.ID,
			SourceTaskID: r.sourceTaskID,
			InputEntryID: inputID,
		},
	}); err != nil {
		return false, err
	}
	stored, _, err := r.AppendEntry(ctx, sessionentry.Entry{
		ID:         inputID,
		AuthorKind: authorKind,
		Payload: sessionentry.Message{
			Role:    role.User,
			Content: []sessionentry.ContentBlock{sessionentry.TextBlock{Text: prompt}},
		},
	})
	if err != nil {
		return true, err
	}
	r.input = stored
	r.deps.Emitter.Emit(coreevent.Event{
		Source:  r.EventSource(""),
		Payload: coreevent.EntriesAppended{Entries: []sessionentry.Entry{*stored}},
	})
	if hookErr := r.deps.HookDispatcher.UserInputCommitted(context.WithoutCancel(ctx), hook.EntryCommitted{
		Identity: runIdentity(r, ""),
		Entry:    *stored,
	}); hookErr != nil {
		r.deps.Logger.Errorf("user input committed hook failed: %v", hookErr)
	}
	return true, nil
}

// SettlePreparation 结束已经写入 RunPrepared、但没有进入 Loop 的执行。
func (r *Run) SettlePreparation(ctx context.Context, err error) error {
	outcome, code, message := r.classifyOutcome(ctx, err)
	endCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	return r.Settle(endCtx, outcome, code, message)
}

// Execute 覆盖本次准备、完整 Loop 和必要收尾；调用方只负责执行位置和后续调度。
func (r *Run) Execute(ctx context.Context, runner *loop.Runner, prepareTools func(context.Context) (loop.ToolExecutor, error)) (err error) {
	if r.status != sessionentry.RunStarting || r.terminal != nil {
		return errorcode.ErrInvalidTransition
	}
	ctx, scope := r.deps.HookDispatcher.Start(ctx, hook.Operation{
		Identity: runIdentity(r, ""),
		Kind:     hook.OperationAgent,
		Name:     r.definition.ID.String(),
	})
	defer func() {
		scope.End(hook.Result{
			Outcome: string(r.Outcome()),
			Err:     err,
		})
	}()
	if err = ctx.Err(); err == nil {
		startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		err = r.ConfirmRunning(startCtx)
		cancel()
	}
	if err == nil {
		if hookErr := r.deps.HookDispatcher.RunStarted(context.WithoutCancel(ctx), hook.RunEvent{Identity: runIdentity(r, "")}); hookErr != nil {
			r.deps.Logger.Errorf("run start hook failed: %v", hookErr)
		}
		if err = ctx.Err(); err == nil {
			var tools loop.ToolExecutor
			tools, err = prepareTools(ctx)
			if err == nil {
				params, deps := r.modelRunParams()
				_, err = runner.Run(ctx, params, deps, r.input, tools)
			}
		}
	}
	if err == nil {
		if _, _, saveErr := r.AppendEntry(context.WithoutCancel(ctx), sessionentry.Entry{
			ID:      "ending:" + r.id.String(),
			Payload: sessionentry.RunEndingStarted{},
		}); saveErr != nil {
			err = errors.Join(err, saveErr)
		} else {
			r.deps.Emitter.Emit(coreevent.Event{
				Source:  r.EventSource(""),
				Payload: coreevent.RunEnding{},
			})
		}
	}
	r.finishChildren()
	outcome, code, message := r.classifyOutcome(ctx, err)
	if !r.Main() && outcome == sessionentry.OutcomeCompleted {
		outcome = sessionentry.OutcomeYielded
	}
	if err != nil && outcome == sessionentry.OutcomeFailed {
		r.emitError(r.currentTurnID, err)
	}
	// 收尾不继承已取消的执行 Context，保存失败明确返回，不自动重投。
	endCtx, cancelEnd := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelEnd()
	if closeErr := r.closeToolCalls(endCtx); closeErr != nil {
		return storageFailure(errors.Join(err, closeErr))
	}
	if saveErr := r.Settle(endCtx, outcome, code, message); saveErr != nil {
		return storageFailure(errors.Join(err, saveErr))
	}
	return err
}

func (r *Run) ConfirmRunning(ctx context.Context) error {
	at := r.deps.Clock.Now()
	_, _, err := r.AppendEntry(ctx, sessionentry.Entry{
		ID:      "started:" + r.id.String(),
		Payload: sessionentry.RunStarted{At: at},
	})
	if err != nil {
		return storageFailure(err)
	}
	r.deps.Emitter.Emit(coreevent.Event{
		Source:  r.EventSource(""),
		Payload: coreevent.RunStarted{StartedAt: at},
	})
	return nil
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
	var payload sessionentry.RunTerminal
	switch outcome {
	case sessionentry.OutcomeCompleted:
		payload = sessionentry.RunCompleted{RunStats: stats}
	case sessionentry.OutcomeYielded:
		payload = sessionentry.SubagentYielded{RunStats: stats}
	case sessionentry.OutcomeFailed:
		payload = sessionentry.RunFailed{
			RunStats:   stats,
			RunFailure: sessionentry.RunFailure{Code: code, Message: message},
		}
	case sessionentry.OutcomeInterrupted:
		payload = sessionentry.RunInterrupted{
			RunStats:   stats,
			RunFailure: sessionentry.RunFailure{Code: code, Message: message},
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
			State:          sessionentry.AgentStateFromOutcome(outcome),
		},
	})
	return nil
}

func (r *Run) classifyOutcome(ctx context.Context, err error) (sessionentry.RunOutcome, sessionentry.RunFailureCode, string) {
	if loop.FailureClassification(context.Cause(ctx)) == loop.FailureStorage {
		return sessionentry.OutcomeFailed, sessionentry.RunFailureStorage, ""
	}
	if err == nil {
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
