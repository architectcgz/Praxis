package run

import (
	"context"
	"errors"
	sessionentry "praxis/internal/core/entry"
	"praxis/internal/core/hook"
	"praxis/internal/core/loop"
	"praxis/internal/errorcode"
	"strings"
	"time"
)

const ErrInterrupted errorcode.Code = "run.interrupted"

// Execute 覆盖本次准备、完整 Loop 和必要收尾；调用方只负责执行位置和后续调度。
func (r *Run) Execute(ctx context.Context, runner *loop.Runner, prepareTools func(context.Context) (loop.ToolExecutor, error)) (err error) {
	if r.Snapshot().Status != sessionentry.RunStarting {
		return errorcode.ErrInvalidTransition
	}
	ctx, scope := r.deps.HookDispatcher.Start(ctx, hook.Operation{
		Identity: runIdentity(r, ""),
		Kind:     hook.OperationAgent,
		Name:     r.Snapshot().Config.AgentDefinitionID.String(),
	})
	defer func() { scope.End(hook.Result{Outcome: string(r.Snapshot().Outcome), Err: err}) }()
	startCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	err = r.ConfirmRunning(startCtx)
	cancel()
	if err == nil {
		if hookErr := r.deps.HookDispatcher.RunStarted(context.WithoutCancel(ctx), hook.RunEvent{Identity: runIdentity(r, "")}); hookErr != nil {
			r.deps.Logger.Errorf("run start hook failed: %v", hookErr)
		}
		if err = ctx.Err(); err == nil {
			var tools loop.ToolExecutor
			tools, err = prepareTools(ctx)
			if err == nil {
				input, _ := r.Entry(r.Snapshot().InputEntryID)
				params, deps := r.modelRunParams()
				_, err = runner.Run(ctx, params, deps, input, tools)
			}
		}
	}
	if err == nil {
		if _, _, saveErr := r.AppendEntry(context.WithoutCancel(ctx), sessionentry.ContextEntry{ID: "ending:" + r.Snapshot().ID.String(), Payload: sessionentry.RunEndingStarted{}}); saveErr != nil {
			err = errors.Join(err, saveErr)
		}
	}
	r.finishChildren()
	outcome, code, message := r.result(ctx, err)
	if !r.Main() && outcome == sessionentry.OutcomeCompleted {
		outcome = sessionentry.OutcomeYielded
	}
	if err != nil {
		r.emitError(r.Snapshot().CurrentTurn.ID, err)
	}
	// 收尾不继承已取消的执行 Context，保存失败明确返回，不自动重投。
	endCtx, cancelEnd := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancelEnd()
	if closeErr := r.closeToolCalls(endCtx); closeErr != nil {
		return errors.Join(err, closeErr)
	}
	_, saveErr := r.Settle(endCtx, outcome, code, message)
	return errors.Join(err, saveErr)
}

func (r *Run) result(ctx context.Context, err error) (sessionentry.RunOutcome, sessionentry.RunFailureCode, string) {
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
