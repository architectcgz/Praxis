package loop

import (
	"context"
	"encoding/json"
	"errors"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
	"praxis/internal/repository"
)

// Dependencies 是 loop 的执行依赖；由组合根装配，不包含跨次执行的可变状态。
type Dependencies struct {
	Entries        repository.Entries
	Provider       providerapi.Provider
	Tools          tool.ToolCatalog
	Progress       ExecutionProgress
	HookDispatcher *hook.Dispatcher
	// CompressionStrategy 由装配层选择，运行期不补充默认策略。
	CompressionStrategy contextcompress.Strategy
	SummaryFormat       contextcompress.SummaryFormat
	Emitter             *coreevent.Emitter
	Logf                func(string, ...any)
}

// Runner 持有装配层提供的共享依赖，可供多次执行并发复用。
type Runner struct {
	Dependencies
}

// NewRunner 保存装配层提供的完整执行依赖。
func NewRunner(deps Dependencies) Runner {
	return Runner{Dependencies: deps}
}

// Run 使用已准备的执行数据和本次执行依赖运行模型与工具循环。
// 恢复、已完成判断和上下文数据加载由业务上层完成；请求和输出记录由 Execution Service 原生负责。
// Loop 不决定执行终态、释放归属或调度后续预约。
func (r *Runner) Run(ctx context.Context, executionInput Input, deps ExecutionDependencies) (Result, error) {
	reason, _, err := r.run(ctx, executionInput, deps)
	return Result{Reason: reason}, err
}

// run 根据本次执行输入运行迭代 loop。每轮只发起一次模型请求；确认自然完成时结束，
// 否则必须先持久化每个工具调用结果，再构造下一次模型请求。
func (r *Runner) run(
	ctx context.Context,
	executionInput Input,
	deps ExecutionDependencies,
) (EndReason, FailureCode, error) {
	modelContext := executionInput.Context

	for {
		request := buildModelRequest(providerapi.Request{
			ExecutionID:     executionInput.ID,
			PromptCacheKey:  executionInput.SessionID.String(),
			Model:           executionInput.Config.Model,
			MaxOutputTokens: executionInput.Config.Model.MaxOutputTokens,
		}, *modelContext, r.Tools)
		toolPayload, err := json.Marshal(request.Tools)
		if err != nil {
			return loopResult(failCause(FailureContract, err))
		}
		compressed, changed, err := r.compressContext(ctx, executionInput, *modelContext, contextcompress.Options{
			SummaryFormat:   r.SummaryFormat,
			ContextWindow:   executionInput.Config.Model.ContextWindow,
			MaxOutputTokens: executionInput.Config.Model.MaxOutputTokens,
			ReservedTokens:  appcontext.EstimateTokens(string(toolPayload)),
		})
		if err != nil {
			err = modelFailure(err)
			r.emitError(executionInput.AgentID, executionInput.ID, "", err)
			return loopResult(err)
		}
		if changed {
			r.Logf("context compressed execution_id=%s entriesBefore=%d entriesAfter=%d", executionInput.ID, len(modelContext.Entries), len(compressed.Entries))
			*modelContext = compressed
			request.Context = modelContext.Clone()
		}
		sequence, err := r.Progress.NextRequestSequence(ctx, executionInput.Run)
		if err != nil {
			return loopResult(entryFailure(executionInput, err))
		}
		turnID := identity.NewTurnID(executionInput.ID, sequence)
		request.TurnID = turnID
		outcome, failureCode, finished, err := r.executeTurn(ctx, executionInput, deps, request, modelContext, sequence)

		if err != nil || outcome != EndCompleted || finished {
			return outcome, failureCode, err
		}
	}
}

// executeTurn 执行一轮模型、工具和回执业务，返回明确的继续/完成决策。
func (r *Runner) executeTurn(
	ctx context.Context,
	executionInput Input,
	deps ExecutionDependencies,
	request providerapi.Request,
	modelContext *appcontext.ModelContext,
	sequence uint64,
) (outcome EndReason, failureCode FailureCode, finished bool, err error) {
	turnID := request.TurnID
	iteration := hook.Iteration{Identity: executionIdentity(executionInput, turnID), Sequence: sequence}
	iteration.OperationID = hook.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeIteration(ctx, iteration); hookErr != nil {
		return endFailed, FailureInternal, false, failCause(FailureInternal, hookErr)
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
		Kind:        coreevent.TurnStarted,
		SessionID:   executionInput.SessionID,
		AgentID:     executionInput.AgentID,
		ExecutionID: executionInput.ID,
		TurnID:      turnID,
	})
	r.Logf("provider request prepared id=%s model=%s contextEntries=%d turn=%s", executionInput.ID, executionInput.Config.Model.ModelID, len(modelContext.Entries), turnID)
	r.Emitter.Emit(coreevent.Event{
		Kind:        coreevent.ProviderWaiting,
		SessionID:   executionInput.SessionID,
		AgentID:     executionInput.AgentID,
		ExecutionID: executionInput.ID,
		TurnID:      turnID,
		Name:        executionInput.Config.Model.ProviderID + "/" + executionInput.Config.Model.ModelID,
	})
	response, err := r.callModel(ctx, executionInput, request, hook.ModelRequestIteration, sequence, func(event providerapi.Event) {
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
			stored, inserted, writeErr := r.appendEntry(context.WithoutCancel(ctx), executionInput, sessionentry.ContextEntry{
				ID: "assistant:" + turnID.String(), RequestID: executionInput.RequestID.String(), ExecutionID: executionInput.ID.String(),
				TurnID: turnID.String(), StopReason: stopReason, Role: role.Assistant, AuthorKind: sessionentry.AuthorAgent,
				AuthorID: executionInput.AgentID.String(), Content: partial,
			})
			if writeErr != nil {
				err = entryFailure(executionInput, errors.Join(err, writeErr))
			} else {
				if inserted {
					r.notifyAssistant(ctx, executionInput, stored)
				}
				*modelContext = modelContext.Append(stored)
				r.Emitter.Emit(coreevent.Event{
					Kind:        coreevent.TurnCompleted,
					SessionID:   executionInput.SessionID,
					AgentID:     executionInput.AgentID,
					ExecutionID: executionInput.ID,
					TurnID:      turnID,
					Entries:     []sessionentry.ContextEntry{stored},
				})
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
		message, inserted, err := r.appendEntry(ctx, executionInput, message)
		if err != nil {
			r.emitError(executionInput.AgentID, executionInput.ID, turnID, err)
			outcome, code, cause := loopResult(entryFailure(executionInput, err))
			return outcome, code, false, cause
		}
		committed = append(committed, message)
		calls = message.ToolCalls(executionInput.SessionID, executionInput.AgentID)
		*modelContext = modelContext.Append(message)
		if inserted {
			r.notifyAssistant(ctx, executionInput, message)
		}
	}
	for _, call := range calls {
		r.Emitter.Emit(coreevent.Event{
			Kind:        coreevent.ToolCall,
			SessionID:   executionInput.SessionID,
			AgentID:     executionInput.AgentID,
			ExecutionID: executionInput.ID,
			TurnID:      turnID,
			CallID:      call.CallID,
			Name:        string(call.Name),
			Input:       append([]byte(nil), call.Arguments...),
		})
	}
	if response.stopReason == providerapi.StopCompleted || response.stopReason == providerapi.StopSequence {

		r.Emitter.Emit(coreevent.Event{
			Kind:        coreevent.TurnCompleted,
			Entries:     committed,
			SessionID:   executionInput.SessionID,
			AgentID:     executionInput.AgentID,
			ExecutionID: executionInput.ID,
			TurnID:      turnID,
			StopReason:  response.stopReason,
		})
		return EndCompleted, "", true, nil
	}
	results, err := r.runToolCalls(ctx, executionInput, deps, calls, turnID)
	if err != nil {
		// 已返回的回执保持原样；当前失败调用和后续未执行调用由本轮退出分支统一收尾。
		interrupted, closeErr := r.interruptToolCalls(ctx, executionInput, calls[len(results):])
		results = append(results, interrupted...)
		if closeErr != nil {
			err = entryFailure(executionInput, errors.Join(err, closeErr))
		}
	}
	committed = append(committed, results...)
	for _, result := range results {
		*modelContext = modelContext.Append(result)
	}
	if err != nil {
		if len(committed) > 0 {
			r.Emitter.Emit(coreevent.Event{
				Kind:        coreevent.TurnCompleted,
				SessionID:   executionInput.SessionID,
				AgentID:     executionInput.AgentID,
				ExecutionID: executionInput.ID,
				TurnID:      turnID,
				Entries:     committed,
			})
		}
		r.emitError(executionInput.AgentID, executionInput.ID, turnID, err)
		outcome, code, cause := loopResult(err)
		return outcome, code, false, cause
	}
	r.Emitter.Emit(coreevent.Event{
		Kind:        coreevent.TurnCompleted,
		Entries:     committed,
		SessionID:   executionInput.SessionID,
		AgentID:     executionInput.AgentID,
		ExecutionID: executionInput.ID,
		TurnID:      turnID,
		StopReason:  response.stopReason,
	})
	return EndCompleted, "", false, nil
}
