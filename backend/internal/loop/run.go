package loop

import (
	"context"
	"encoding/json"
	"errors"
	agentmodel "praxis/internal/core/agent"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Dependencies 是 loop 的执行依赖；由组合根装配，不包含跨次执行的可变状态。
type Dependencies struct {
	ModelBuilder   modelapi.ModelBuilder
	Tools          tool.ToolCatalog
	Requests       RequestSequencer
	HookDispatcher *hook.Dispatcher
	// CompressionStrategy 为空时使用完整远程压缩；其他策略通过依赖注入替换。
	CompressionStrategy contextcompress.Strategy
	SummaryFormat       contextcompress.SummaryFormat
	EventObserver       EventObserver
	Logf                func(string, ...any)
}

// Runner 持有已校验的共享依赖，可供多次执行并发复用。
type Runner struct {
	Dependencies
}

// NewRunner 校验并冻结 loop 的执行依赖。缺少任一必要依赖时直接返回错误，
// 使装配失败的执行器无法进入调度，而不是在每次执行时才发现。
func NewRunner(deps Dependencies) (Runner, error) {
	if deps.ModelBuilder == nil {
		return Runner{}, errors.New("loop model builder is required")
	}
	if deps.Tools == nil {
		return Runner{}, errors.New("loop tool catalog is required")
	}
	if deps.Requests == nil {
		return Runner{}, errors.New("loop request sequencer is required")
	}
	if !deps.HookDispatcher.HasModelHooks() {
		return Runner{}, errors.New("loop model request hooks are required")
	}
	if deps.EventObserver == nil {
		return Runner{}, errors.New("loop event observer is required")
	}
	if deps.Logf == nil {
		deps.Logf = func(string, ...any) {}
	}
	if deps.CompressionStrategy == nil {
		deps.CompressionStrategy = contextcompress.FullStrategy{}
	}
	return Runner{Dependencies: deps}, nil
}

// Run 使用已准备的执行数据和本次执行依赖运行模型与工具循环。
// 恢复、已完成判断和上下文数据加载由业务上层完成；请求和输出记录由同步 Hook 负责。
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
	if deps.ToolExecutor == nil || deps.Assistant == nil {
		return loopResult(fail(FailureContract, ErrorContract, "scoped tool execution and assistant committer are required"))
	}
	if executionInput.Context == nil {
		return loopResult(fail(FailureContract, ErrorContract, "session context is required"))
	}
	modelContext := executionInput.Context
	if executionInput.InputEntry != nil {
		r.emit(Event{Kind: EventEntriesAppended, SessionID: executionInput.SessionID, AgentID: executionInput.AgentID, ExecutionID: executionInput.ID, Entries: []sessionentry.ContextEntry{*executionInput.InputEntry}})
	}
	builtModel, err := r.buildModel(executionInput.Config.Model)
	if err != nil {
		r.emitError(executionInput.AgentID, executionInput.ID, "", err)
		return loopResult(err)
	}

	for {
		request := buildModelRequest(modelapi.ModelRequest{
			ExecutionID:     executionInput.ID,
			PromptCacheKey:  executionInput.SessionID.String(),
			Model:           executionInput.Config.Model,
			MaxOutputTokens: builtModel.MaxOutputTokens,
		}, *modelContext, r.Tools)
		toolPayload, err := json.Marshal(request.Tools)
		if err != nil {
			return loopResult(failCause(FailureContract, err))
		}
		compressed, changed, err := r.CompressionStrategy.Compress(ctx, *modelContext, contextcompress.Options{
			SummaryFormat:   r.SummaryFormat,
			ContextWindow:   executionInput.Config.Model.ContextWindow,
			MaxOutputTokens: builtModel.MaxOutputTokens,
			ReservedTokens:  appcontext.EstimateTokens(string(toolPayload)),
		}, func(summaryCtx context.Context, history appcontext.ModelContext, limit int) (string, error) {
			return r.summarizeContext(summaryCtx, executionInput, builtModel.Stream, history, limit)
		})
		if err != nil {
			err = modelFailure(err)
			r.emitError(executionInput.AgentID, executionInput.ID, "", err)
			return loopResult(err)
		}
		if changed {
			for index, entry := range compressed.Entries {
				if entry.Kind != sessionentry.ContextEntrySummary || entry.ID != "" {
					continue
				}
				stored, err := deps.Assistant.AppendSummary(ctx, entry)
				if err != nil {
					return loopResult(failCause(FailureStorage, err))
				}
				compressed.Entries[index] = stored
				r.emit(Event{Kind: EventEntriesAppended, SessionID: executionInput.SessionID, AgentID: executionInput.AgentID, ExecutionID: executionInput.ID, Entries: []sessionentry.ContextEntry{stored}})
			}
			r.Logf("context compressed execution_id=%s entriesBefore=%d entriesAfter=%d", executionInput.ID, len(modelContext.Entries), len(compressed.Entries))
			*modelContext = compressed
			request.Context = modelContext.Clone()
		}
		sequence, err := r.Requests.NextRequestSequence(ctx, executionInput.ID)
		if err != nil {
			return loopResult(failCause(FailureStorage, err))
		}
		turnID := identity.NewTurnID(executionInput.ID, sequence)
		request.TurnID = turnID
		outcome, failureCode, finished, err := r.executeTurn(ctx, executionInput, deps, builtModel, request, modelContext, sequence)

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
	builtModel modelapi.Model,
	request modelapi.ModelRequest,
	modelContext *appcontext.ModelContext,
	sequence uint64,
) (EndReason, FailureCode, bool, error) {
	turnID := request.TurnID
	r.emit(Event{
		Kind:        EventTurnStarted,
		SessionID:   executionInput.SessionID,
		AgentID:     executionInput.AgentID,
		ExecutionID: executionInput.ID,
		TurnID:      turnID,
	})
	r.Logf("provider request prepared id=%s model=%s contextEntries=%d turn=%s", executionInput.ID, executionInput.Config.Model.ModelID, len(modelContext.Entries), turnID)
	r.emit(Event{
		Kind:        EventProviderWaiting,
		SessionID:   executionInput.SessionID,
		AgentID:     executionInput.AgentID,
		ExecutionID: executionInput.ID,
		TurnID:      turnID,
		Name:        executionInput.Config.Model.ProviderID + "/" + executionInput.Config.Model.ModelID,
	})
	response, err := requestModel(ctx, builtModel.Stream, request, hook.ModelRequest{
		Identity: hook.Identity{
			SessionID:   executionInput.SessionID,
			AgentID:     executionInput.AgentID,
			ExecutionID: executionInput.ID,
			TurnID:      turnID,
		},
		Kind:     hook.ModelRequestIteration,
		Sequence: sequence,
	}, r.HookDispatcher, func(event modelapi.ModelStreamEvent) {
		r.emitModelEvent(executionInput.SessionID, executionInput.AgentID, executionInput.ID, turnID, event)
	})
	if err != nil {
		blocks := modelapi.EntryBlocksFromEvents(response.events)
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
			stored, _, writeErr := deps.Assistant.AppendAssistant(context.WithoutCancel(ctx), AssistantWrite{Entry: sessionentry.ContextEntry{
				ID: "assistant:" + turnID.String(), RequestID: executionInput.RequestID.String(), ExecutionID: executionInput.ID.String(),
				TurnID: turnID.String(), StopReason: stopReason, Role: sessionentry.RoleAssistant, AuthorKind: sessionentry.AuthorAgent,
				AuthorID: executionInput.AgentID.String(), Content: partial,
			}})
			if writeErr != nil {
				err = errors.Join(err, writeErr)
			} else {
				*modelContext = modelContext.Append(stored)
				r.emit(Event{Kind: EventTurnCompleted, SessionID: executionInput.SessionID, AgentID: executionInput.AgentID, ExecutionID: executionInput.ID, TurnID: turnID, Entries: []sessionentry.ContextEntry{stored}})
			}
		}
		err = modelFailure(err)
		r.emitError(executionInput.AgentID, executionInput.ID, turnID, err)
		outcome, code, cause := loopResult(err)
		return outcome, code, false, cause
	}

	assistant := modelapi.EntryBlocksFromEvents(response.events)
	var calls []sessionentry.ToolCall
	var committed []sessionentry.ContextEntry

	{
		message := sessionentry.ContextEntry{
			ID:          "assistant:" + turnID.String(),
			RequestID:   executionInput.RequestID.String(),
			ExecutionID: executionInput.ID.String(),
			TurnID:      turnID.String(),
			StopReason:  string(response.stopReason),
			Role:        sessionentry.RoleAssistant,
			AuthorKind:  sessionentry.AuthorAgent,
			AuthorID:    executionInput.AgentID.String(),
			Content:     assistant,
		}
		value := AssistantWrite{Entry: message}
		if response.stopReason == modelapi.StopCompleted || response.stopReason == modelapi.StopSequence {
			value.FinalOutput = &agentmodel.FinalOutput{AssistantID: message.ID, StopReason: message.StopReason}
		}
		message, inserted, err := deps.Assistant.AppendAssistant(ctx, value)
		if err != nil {
			r.emitError(executionInput.AgentID, executionInput.ID, turnID, err)
			outcome, code, cause := loopResult(failCause(FailureStorage, err))
			return outcome, code, false, cause
		}
		committed = append(committed, message)
		calls = message.ToolCalls(executionInput.SessionID, executionInput.AgentID)
		*modelContext = modelContext.Append(message)
		if inserted && value.FinalOutput != nil {
			if hookErr := r.HookDispatcher.OnCommitted(context.WithoutCancel(ctx), hook.CommittedEvent{
				Identity: hook.Identity{
					SessionID:   executionInput.SessionID,
					AgentID:     executionInput.AgentID,
					ExecutionID: executionInput.ID,
					TurnID:      turnID,
				},
				Entry: &message,
			}); hookErr != nil {
				r.Logf("assistant committed hook failed: %v", hookErr)
			}
		}
	}
	for _, call := range calls {
		r.emit(Event{
			Kind:        EventToolCall,
			SessionID:   executionInput.SessionID,
			AgentID:     executionInput.AgentID,
			ExecutionID: executionInput.ID,
			TurnID:      turnID,
			CallID:      call.CallID,
			Name:        string(call.Name),
			Input:       append([]byte(nil), call.Arguments...),
		})
	}
	if response.stopReason == modelapi.StopCompleted || response.stopReason == modelapi.StopSequence {

		r.emit(Event{
			Kind:        EventTurnCompleted,
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
	committed = append(committed, results...)
	if err != nil {
		if len(committed) > 0 {
			r.emit(Event{Kind: EventTurnCompleted, SessionID: executionInput.SessionID, AgentID: executionInput.AgentID, ExecutionID: executionInput.ID, TurnID: turnID, Entries: committed})
		}
		r.emitError(executionInput.AgentID, executionInput.ID, turnID, err)
		outcome, code, cause := loopResult(err)
		return outcome, code, false, cause
	}
	for _, result := range results {
		*modelContext = modelContext.Append(result)
	}

	r.emit(Event{
		Kind:        EventTurnCompleted,
		Entries:     committed,
		SessionID:   executionInput.SessionID,
		AgentID:     executionInput.AgentID,
		ExecutionID: executionInput.ID,
		TurnID:      turnID,
		StopReason:  response.stopReason,
	})
	return EndCompleted, "", false, nil
}

func (r *Runner) buildModel(config modelapi.Config) (modelapi.Model, error) {
	built, err := r.ModelBuilder.BuildModel(config)
	if err != nil {
		return modelapi.Model{}, failCause(FailureProvider, err)
	}
	if built.Stream == nil {
		return modelapi.Model{}, fail(FailureProviderUnavailable, ErrorProvider, "model stream is unavailable")
	}
	return built, nil
}
