package loop

import (
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/clock"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/hook"
	messagemodel "praxis/internal/core/message"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolcall"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// Dependencies 是 loop 的执行依赖；由组合根装配，不包含跨次执行的可变状态。
type Dependencies struct {
	IDs            identity.IDGenerator
	Clock          clock.Clock
	Executions     ToolExecutionReader
	ModelBuilder   modelapi.ModelBuilder
	Tools          tool.ToolCatalog
	ToolExecutor   ToolExecutor
	HookDispatcher *hook.Dispatcher
	// CompressionStrategy 为空时使用完整远程压缩；其他策略通过依赖注入替换。
	CompressionStrategy contextcompress.Strategy
	SummaryFormat       contextcompress.SummaryFormat
	EventObserver       EventObserver
	Logf                func(string, ...any)
}

// Runner 是持有已校验依赖的 loop 执行器，可跨多次 执行输入 执行复用。
type Runner struct {
	Dependencies
}

// NewRunner 校验并冻结 loop 的执行依赖。缺少任一必要依赖时直接返回错误，
// 使装配失败的执行器无法进入调度，而不是在每次执行时才发现。
func NewRunner(deps Dependencies) (Runner, error) {
	if deps.Executions == nil {
		return Runner{}, errors.New("loop tool execution repository is required")
	}
	if deps.ModelBuilder == nil {
		return Runner{}, errors.New("loop model builder is required")
	}
	if deps.Tools == nil {
		return Runner{}, errors.New("loop tool catalog is required")
	}
	if deps.ToolExecutor == nil {
		return Runner{}, errors.New("loop tool call handler is required")
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
	deps.IDs = identity.IDsOrDefault(deps.IDs)
	deps.Clock = clock.ClockOrDefault(deps.Clock)
	return Runner{Dependencies: deps}, nil
}

// Run 使用本次执行输入与消息写入器运行 provider/tool loop，返回结果及失败分类。
// 业务上层提供已选通用内容与续接消息，消息写入器负责可靠提交输出；
// ProgressStore 负责加载持久化预算、记录模型及摘要请求和模型输出消耗；
// 每次执行的输入（context、消息写入器）为空时返回错误；
// 持久化生命周期、取消和后续任务调度由执行协调器负责。
func (r *Runner) Run(ctx context.Context, input Input) (Result, error) {
	reason, _, err := r.run(ctx, input, input.Content, input.Continuation, input.Writer)
	return Result{Reason: reason}, err
}

// run 是 input 的迭代 loop。每轮只发起一次模型请求；确认自然完成时结束，
// 否则必须先持久化每个工具调用结果，再构造下一次模型请求。
func (r *Runner) run(
	ctx context.Context,
	input Input,
	content appcontext.Content,
	continuation []messagemodel.Message,
	messageWriter toolcall.Writer,
) (EndReason, FailureCode, error) {
	progress, err := input.Progress.LoadProgress(ctx)
	if err != nil {
		return loopResult(failCause(FailureStorage, err))
	}
	modelContext, err := (appcontext.ContextBuilder{}).Build(content, appcontext.Limits{ContextWindow: input.Config.Model.ContextWindow, MaxOutputTokens: input.Config.Model.MaxOutputTokens})
	if err != nil {
		return loopResult(modelFailure(err))
	}
	if progress.FinalStopReason != "" {
		return EndCompleted, "", nil
	}
	if err := r.resumeToolCalls(ctx, input, continuation, messageWriter, &modelContext); err != nil {
		if errors.Is(err, errAwaitingApproval) {
			return EndAwaitingApproval, "", nil
		}
		return loopResult(err)
	}

	builtModel, err := r.buildModel(input.Config.Model)
	if err != nil {
		r.emitError(input.AgentID, input.ID, "", err)
		return loopResult(err)
	}

	iterations := progress.Iterations
	for sequence := max(uint64(1), progress.NextSequence); ; {
		if iterations >= MaxIterations {
			return loopResult(fail(FailureResourceLimit, ErrorResourceLimit, "model iteration limit exceeded"))
		}
		request := buildModelRequest(modelapi.ModelRequest{
			ExecutionID:     input.ID,
			PromptCacheKey:  input.SessionID.String(),
			Model:           input.Config.Model,
			MaxOutputTokens: builtModel.MaxOutputTokens,
		}, modelContext, r.Tools)
		toolPayload, err := json.Marshal(request.Tools)
		if err != nil {
			return loopResult(failCause(FailureContract, err))
		}
		compressed, changed, err := r.CompressionStrategy.Compress(ctx, modelContext, contextcompress.Options{
			SummaryFormat:   r.SummaryFormat,
			ContextWindow:   input.Config.Model.ContextWindow,
			MaxOutputTokens: builtModel.MaxOutputTokens,
			ReservedTokens:  appcontext.EstimateTokens(string(toolPayload)),
		}, func(summaryCtx context.Context, history appcontext.ModelContext, limit int) (string, error) {
			current := sequence
			sequence++
			return r.summarizeContext(summaryCtx, input, builtModel.Stream, history, limit, current)
		})
		if err != nil {
			err = modelFailure(err)
			r.emitError(input.AgentID, input.ID, "", err)
			return loopResult(err)
		}
		if changed {
			r.Logf("context compressed input=%s entriesBefore=%d entriesAfter=%d", input.ID, len(modelContext.Entries), len(compressed.Entries))
			modelContext = compressed
			request.Context = modelContext.Clone()
		}
		turnSequence := sequence
		if err := input.Progress.RecordIteration(ctx, turnSequence); err != nil {
			if errors.Is(err, fault.New(ErrorResourceLimit, "")) {
				return loopResult(failCause(FailureResourceLimit, err))
			}
			return loopResult(failCause(FailureStorage, err))
		}
		sequence++
		iterations++
		turnID := identity.NewTurnID(input.ID, turnSequence)
		request.TurnID = turnID
		outcome, failureCode, finished, err := r.executeTurn(ctx, input, builtModel, messageWriter, request, &modelContext, turnID)

		if err != nil || outcome != EndCompleted || finished {
			return outcome, failureCode, err
		}
		// 工具的独立风险审核也会分配请求序号，下一轮不能复用本地旧序号。
		stored, err := input.Progress.LoadProgress(ctx)
		if err != nil {
			return loopResult(failCause(FailureStorage, err))
		}
		sequence = max(sequence, stored.NextSequence)
	}
}

// executeTurn 执行一轮模型、工具和回执业务，返回明确的继续/完成决策。
func (r *Runner) executeTurn(
	ctx context.Context,
	input Input,
	builtModel modelapi.Model,
	messageWriter toolcall.Writer,
	request modelapi.ModelRequest,
	modelContext *appcontext.ModelContext,
	turnID identity.TurnID,
) (EndReason, FailureCode, bool, error) {
	r.emit(Event{
		Kind:        EventTurnStarted,
		SessionID:   input.SessionID,
		AgentID:     input.AgentID,
		ExecutionID: input.ID,
		TurnID:      turnID,
	})
	r.Logf("provider request prepared id=%s model=%s contextEntries=%d turn=%s", input.ID, input.Config.Model.ModelID, len(modelContext.Entries), turnID)
	r.emit(Event{
		Kind:        EventProviderWaiting,
		SessionID:   input.SessionID,
		AgentID:     input.AgentID,
		ExecutionID: input.ID,
		TurnID:      turnID,
		Name:        input.Config.Model.ProviderID + "/" + input.Config.Model.ModelID,
	})
	response, err := requestModel(ctx, builtModel.Stream, request, hook.Identity{
		SessionID:   input.SessionID,
		AgentID:     input.AgentID,
		ExecutionID: input.ID,
		TurnID:      turnID,
	}, r.HookDispatcher, func(event modelapi.ModelStreamEvent) {
		r.emitModelEvent(input.SessionID, input.AgentID, input.ID, turnID, event)
	})
	if err != nil {
		err = modelFailure(err)
		r.emitError(input.AgentID, input.ID, turnID, err)
		outcome, code, cause := loopResult(err)
		return outcome, code, false, cause
	}
	if err := input.Progress.RecordModelOutput(ctx, response.textBytes); err != nil {
		outcome, code, cause := loopResult(failCause(FailureStorage, err))
		return outcome, code, false, cause
	}
	assistant := modelapi.ContextBlocksFromEvents(response.events)
	calls := response.toolCalls()
	metadata := toolcall.ToolExecutionMetadata{
		ExecutionID:   input.ID,
		TurnID:        turnID,
		SessionID:     input.SessionID,
		AgentID:       input.AgentID,
		WorkspacePath: input.Config.WorkspacePath,
	}
	{
		message := outputMessage(messagemodel.Message{
			ID:          "assistant:" + turnID.String(),
			RequestID:   input.RequestID.String(),
			ExecutionID: input.ID.String(),
			TurnID:      turnID.String(),
			StopReason:  string(response.stopReason),
			Role:        messagemodel.RoleAssistant,
			AuthorKind:  messagemodel.AuthorAgent,
			AuthorID:    input.AgentID.String(),
		}, assistant)
		value := toolcall.AssistantWrite{Message: message}
		for _, call := range calls {
			invocation := toolcall.NewExecution(identity.ToolExecutionID(r.IDs.New("toolcall")), input.ID, turnID, input.SessionID, input.AgentID, call.ID, call.Name, call.Arguments, toolcall.ArgumentsDigest(call.Name, call.Arguments), r.Clock.Now())
			value.ToolCalls = append(value.ToolCalls, invocation)
		}
		if response.stopReason == modelapi.StopCompleted || response.stopReason == modelapi.StopSequence {
			value.FinalOutput = &toolcall.FinalOutput{AssistantID: message.ID, StopReason: message.StopReason}
		}
		message, inserted, err := messageWriter.AppendAssistant(ctx, value)
		if err != nil {
			r.emitError(input.AgentID, input.ID, turnID, err)
			outcome, code, cause := loopResult(failCause(FailureStorage, err))
			return outcome, code, false, cause
		}
		if inserted && value.FinalOutput != nil {
			if hookErr := r.HookDispatcher.OnCommitted(context.WithoutCancel(ctx), hook.CommittedEvent{
				Identity: hook.Identity{
					SessionID:   input.SessionID,
					AgentID:     input.AgentID,
					ExecutionID: input.ID,
					TurnID:      turnID,
				},
				Message: &message,
			}); hookErr != nil {
				r.Logf("assistant committed hook failed: %v", hookErr)
			}
		}
	}
	for _, call := range calls {
		r.emit(Event{
			Kind:        EventToolCall,
			SessionID:   input.SessionID,
			AgentID:     input.AgentID,
			ExecutionID: input.ID,
			TurnID:      turnID,
			CallID:      call.ID,
			Name:        string(call.Name),
			Input:       append([]byte(nil), call.Arguments...),
		})
	}
	*modelContext = modelContext.AppendProviderOutput(assistant)
	if response.stopReason == modelapi.StopCompleted || response.stopReason == modelapi.StopSequence {

		r.emit(Event{
			Kind:        EventTurnCompleted,
			SessionID:   input.SessionID,
			AgentID:     input.AgentID,
			ExecutionID: input.ID,
			TurnID:      turnID,
			StopReason:  response.stopReason,
		})
		return EndCompleted, "", true, nil
	}
	results, err := r.runToolCalls(ctx, messageWriter, calls, metadata, input.RequestID)
	if err != nil {
		if errors.Is(err, errAwaitingApproval) {
			return EndAwaitingApproval, "", false, nil
		}
		r.emitError(input.AgentID, input.ID, turnID, err)
		outcome, code, cause := loopResult(err)
		return outcome, code, false, cause
	}
	for index, result := range results {
		*modelContext = modelContext.AppendToolResult(appcontext.NewToolResultBlock(
			calls[index].ID,
			string(calls[index].Name),
			result.Payload,
			result.ErrorClass != "",
		))
	}

	r.emit(Event{
		Kind:        EventTurnCompleted,
		SessionID:   input.SessionID,
		AgentID:     input.AgentID,
		ExecutionID: input.ID,
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
