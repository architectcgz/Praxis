package loop

import (
	"bytes"
	"context"
	"errors"
	appcontext "praxis/internal/core/context"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Dependencies 是 loop 的执行依赖；由组合根装配，不包含跨次执行的可变状态。
type Dependencies struct {
	Provider       providerapi.Provider
	Tools          tool.ToolCatalog
	HookDispatcher *hook.Dispatcher
	Emitter        *coreevent.Emitter
	Logf           func(string, ...any)
}

// Runner 持有装配层提供的共享依赖，可供多次执行并发复用。
type Runner struct {
	Dependencies
}

// NewRunner 保存装配层提供的完整执行依赖。
func NewRunner(deps Dependencies) Runner {
	return Runner{Dependencies: deps}
}

// Run 消费独立模型上下文，通过本次执行能力提交结果并取得下一轮上下文。
// 恢复、完成证据、历史投影与容量准备由执行层负责；Loop 不持有运行实例或直接访问仓储。
// Loop 不决定执行终态、释放归属或调度后续预约。
func (r *Runner) Run(ctx context.Context, executionInput Input, deps ExecutionDependencies) (Result, error) {
	reason, _, err := r.run(ctx, executionInput, deps)
	return Result{Reason: reason}, err
}

// run 每轮使用准备好的上下文请求模型；确认自然完成时结束。
// 工具回执全部提交成功后才取得下一轮上下文，任何提交或准备失败都会停止循环。
func (r *Runner) run(
	ctx context.Context,
	executionInput Input,
	deps ExecutionDependencies,
) (EndReason, FailureCode, error) {
	modelContext := executionInput.Context

	for {
		tools := r.Tools.List()
		prepared, err := deps.Execution.PrepareContext(ctx, modelContext, tools, func(summaryCtx context.Context, value appcontext.ModelContext, limit int) (string, error) {
			return r.summarizeContext(summaryCtx, executionInput, deps, value, limit)
		})
		if err != nil {
			err = modelFailure(err)
			r.emitError(executionInput.AgentID, executionInput.ID, "", err)
			return loopResult(err)
		}
		sequence, err := deps.Execution.NextRequestSequence(ctx)
		if err != nil {
			return loopResult(err)
		}
		request := providerapi.Request{
			ExecutionID:     executionInput.ID,
			PromptCacheKey:  executionInput.SessionID.String(),
			Model:           executionInput.Model.Clone(),
			Context:         prepared.Clone(),
			Tools:           tools,
			MaxOutputTokens: executionInput.Model.MaxOutputTokens,
			TurnID:          identity.NewTurnID(executionInput.ID, sequence),
		}
		outcome, failureCode, finished, err := r.executeTurn(ctx, executionInput, deps, request, sequence)

		if err != nil || outcome != EndCompleted || finished {
			return outcome, failureCode, err
		}
		modelContext, err = deps.Execution.NextContext(ctx)
		if err != nil {
			err = modelFailure(err)
			r.emitError(executionInput.AgentID, executionInput.ID, "", err)
			return loopResult(err)
		}
	}
}

// executeTurn 执行一轮模型、工具和回执业务，返回明确的继续/完成决策。
func (r *Runner) executeTurn(
	ctx context.Context,
	executionInput Input,
	deps ExecutionDependencies,
	request providerapi.Request,
	sequence uint64,
) (outcome EndReason, failureCode FailureCode, finished bool, err error) {
	turnID := request.TurnID
	iteration := hook.Iteration{Identity: executionIdentity(executionInput, turnID), Sequence: sequence}
	iteration.OperationID = hook.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeIteration(ctx, iteration); hookErr != nil {
		return endFailed, FailureInternal, false, NewFailure(FailureInternal, hookErr)
	}
	var stopReason providerapi.StopReason
	defer func() {
		if hookErr := r.HookDispatcher.AfterIteration(context.WithoutCancel(ctx), hook.IterationResult{
			Iteration:  iteration,
			StopReason: stopReason,
			Err:        err,
		}); hookErr != nil {
			r.Logf("after iteration hook failed: %v", hookErr)
		}
	}()
	r.Emitter.Emit(coreevent.Event{
		Scope: coreevent.Scope{
			SessionID:   executionInput.SessionID,
			AgentID:     executionInput.AgentID,
			ExecutionID: executionInput.ID,
			TurnID:      turnID,
		},
		Payload: coreevent.TurnStarted{},
	})
	r.Logf("provider request prepared id=%s model=%s contextEntries=%d turn=%s", executionInput.ID, executionInput.Model.ModelID, len(request.Context.Entries), turnID)
	r.Emitter.Emit(coreevent.Event{
		Scope: coreevent.Scope{
			SessionID:   executionInput.SessionID,
			AgentID:     executionInput.AgentID,
			ExecutionID: executionInput.ID,
			TurnID:      turnID,
		},
		Payload: coreevent.ProviderWaiting{},
	})
	response, err := r.callModel(ctx, executionInput, deps, request, hook.ModelRequestIteration, sequence, func(event providerapi.Event) {
		r.emitModelEvent(executionInput.SessionID, executionInput.AgentID, executionInput.ID, turnID, event)
	})
	stopReason = response.stopReason
	if err != nil {
		blocks := entryBlocksFromEvents(response.events)
		partial := make([]sessionentry.Block, 0, len(blocks))
		for _, block := range blocks {
			if block.Kind == sessionentry.BlockText || block.Kind == sessionentry.BlockThinking {
				partial = append(partial, block)
			}
		}
		if len(partial) > 0 {
			stopReason := "error"
			if ctx.Err() != nil {
				stopReason = "aborted"
			}
			stored, _, writeErr := deps.Execution.AppendEntry(context.WithoutCancel(ctx), sessionentry.ContextEntry{
				ID:          "assistant:" + turnID.String(),
				RequestID:   executionInput.RequestID.String(),
				ExecutionID: executionInput.ID.String(),
				TurnID:      turnID.String(),
				StopReason:  stopReason,
				Role:        role.Assistant,
				AuthorKind:  sessionentry.AuthorAgent,
				AuthorID:    executionInput.AgentID.String(),
				Content:     partial,
			})
			if writeErr != nil {
				err = deps.Execution.EntryFailure(errors.Join(err, writeErr))
			} else {
				r.emitTurnCompletion(executionInput, deps, turnID, []sessionentry.ContextEntry{stored}, providerapi.StopReason(stopReason))
			}
		}
		err = modelFailure(err)
		r.emitError(executionInput.AgentID, executionInput.ID, turnID, err)
		outcome, code, cause := loopResult(err)
		return outcome, code, false, cause
	}

	assistant := entryBlocksFromEvents(response.events)
	var calls []sessionentry.ToolCall
	var committed []sessionentry.ContextEntry

	{
		message := sessionentry.ContextEntry{
			ID:          "assistant:" + turnID.String(),
			RequestID:   executionInput.RequestID.String(),
			ExecutionID: executionInput.ID.String(),
			TurnID:      turnID.String(),
			StopReason:  string(response.stopReason),
			Role:        role.Assistant,
			AuthorKind:  sessionentry.AuthorAgent,
			AuthorID:    executionInput.AgentID.String(),
			Content:     assistant,
		}
		message, _, err := deps.Execution.AppendEntry(ctx, message)
		if err != nil {
			r.emitError(executionInput.AgentID, executionInput.ID, turnID, err)
			outcome, code, cause := loopResult(err)
			return outcome, code, false, cause
		}
		committed = append(committed, message)
		calls = message.ToolCalls(executionInput.SessionID, executionInput.AgentID)
	}
	for _, call := range calls {
		r.Emitter.Emit(coreevent.Event{
			Scope: coreevent.Scope{
				SessionID:   executionInput.SessionID,
				AgentID:     executionInput.AgentID,
				ExecutionID: executionInput.ID,
				TurnID:      turnID,
			},
			Payload: coreevent.ToolCall{
				CallID: call.CallID,
				Name:   string(call.Name),
				Input:  bytes.Clone(call.Arguments),
			},
		})
	}
	if response.stopReason == providerapi.StopCompleted || response.stopReason == providerapi.StopSequence {
		r.emitTurnCompletion(executionInput, deps, turnID, committed, response.stopReason)
		return EndCompleted, "", true, nil
	}
	results, err := r.runToolCalls(ctx, executionInput, deps, calls, turnID)
	if err != nil {
		// 已返回的回执保持原样；当前失败调用和后续未执行调用由本轮退出分支统一收尾。
		interrupted, closeErr := r.interruptToolCalls(ctx, executionInput, deps, calls[len(results):])
		results = append(results, interrupted...)
		if closeErr != nil {
			err = deps.Execution.EntryFailure(errors.Join(err, closeErr))
		}
	}
	committed = append(committed, results...)
	if err != nil {
		if len(committed) > 0 {
			r.emitTurnCompletion(executionInput, deps, turnID, committed, response.stopReason)
		}
		r.emitError(executionInput.AgentID, executionInput.ID, turnID, err)
		outcome, code, cause := loopResult(err)
		return outcome, code, false, cause
	}
	r.emitTurnCompletion(executionInput, deps, turnID, committed, response.stopReason)
	return EndCompleted, "", false, nil
}
