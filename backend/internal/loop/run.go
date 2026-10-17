package loop

import (
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	toolmodel "praxis/internal/core/tool_execution"
	"praxis/internal/hooks"
	"praxis/internal/repository"
	"praxis/internal/system"
	toolcontracts "praxis/internal/tools/contracts"

	"context"
	"encoding/json"
	"errors"
)

// Config 是 loop 的执行依赖；由组合根装配，不包含跨次执行的可变状态。
type Config struct {
	IDs            system.IDGenerator
	Clock          system.Clock
	Executions     ToolExecutionReader
	Progress       TaskProgress
	ModelBuilder   model.ModelBuilder
	Tools          toolcontracts.ToolCatalog
	ToolExecutor   ToolExecutor
	HookDispatcher *hooks.Dispatcher
	// CompressionStrategy 为空时使用完整远程压缩；其他策略通过依赖注入替换。
	CompressionStrategy contextcompress.Strategy
	SummaryFormat       contextcompress.SummaryFormat
	EventObserver       EventObserver
	Logf                func(string, ...any)
}

// Runner 是持有已校验依赖的 loop 执行器，可跨多次 Task 执行复用。
type Runner struct {
	Config
}

// NewRunner 校验并冻结 loop 的执行依赖。缺少任一必要依赖时直接返回错误，
// 使装配失败的执行器无法进入调度，而不是在每次执行时才发现。
func NewRunner(config Config) (Runner, error) {
	if config.Executions == nil {
		return Runner{}, errors.New("loop tool execution repository is required")
	}
	if config.Progress == nil {
		return Runner{}, errors.New("loop task progress is required")
	}
	if config.ModelBuilder == nil {
		return Runner{}, errors.New("loop model builder is required")
	}
	if config.Tools == nil {
		return Runner{}, errors.New("loop tool catalog is required")
	}
	if config.ToolExecutor == nil {
		return Runner{}, errors.New("loop tool call handler is required")
	}
	if config.EventObserver == nil {
		return Runner{}, errors.New("loop event observer is required")
	}
	if config.Logf == nil {
		config.Logf = func(string, ...any) {}
	}
	if config.CompressionStrategy == nil {
		config.CompressionStrategy = contextcompress.FullStrategy{}
	}
	config.IDs = system.IDsOrDefault(config.IDs)
	config.Clock = system.ClockOrDefault(config.Clock)
	return Runner{Config: config}, nil
}

// Run 使用本次执行输入与消息写入器运行 provider/tool loop，返回结果及失败分类。
// 业务上层提供已选通用内容与续接消息，消息写入器负责可靠提交输出；
// TaskProgress 负责加载持久化预算、记录模型及摘要请求和模型输出消耗；
// 每次执行的输入（context、消息写入器）为空时返回错误；
// 持久化生命周期、取消和后续任务调度由执行协调器负责。
func (r *Runner) Run(ctx context.Context, task taskmodel.Task, content appcontext.Content, continuation []sessionmodel.MessageData, messageWriter repository.MessageWriter) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	if ctx == nil {
		return failedTask(errors.New("loop context is required"))
	}
	if messageWriter == nil {
		return failedTask(errors.New("loop message recorder is required"))
	}
	return r.run(ctx, task, content, continuation, messageWriter)
}

// run 是 task 的迭代 loop。每轮只发起一次模型请求；确认自然完成时结束，
// 否则必须先持久化每个工具调用结果，再构造下一次模型请求。
func (r *Runner) run(
	ctx context.Context,
	task taskmodel.Task,
	content appcontext.Content,
	continuation []sessionmodel.MessageData,
	messageWriter repository.MessageWriter,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	task, err := r.Progress.LoadTask(ctx, task.ID)
	if err != nil {
		return taskResult(failCause(contracts.TaskFailureStorage, err))
	}
	modelContext, err := (appcontext.ContextBuilder{}).Build(content, appcontext.Limits{ContextWindow: task.Config.Model.ContextWindow, MaxOutputTokens: task.Config.Model.MaxOutputTokens})
	if err != nil {
		return taskResult(modelFailure(err))
	}
	if task.Progress.FinalStopReason != "" {
		return taskmodel.TaskCompleted, "", nil
	}
	if err := r.resumeToolCalls(ctx, task, continuation, messageWriter, &modelContext); err != nil {
		if errors.Is(err, errAwaitingApproval) {
			return taskmodel.TaskAwaitingApproval, "", nil
		}
		return taskResult(err)
	}

	taskModel, err := r.buildModel(task.Config.Model)
	if err != nil {
		r.emitError(task.AgentID, task.ID, "", err)
		return taskResult(err)
	}

	iterations := task.Progress.Iterations
	for sequence := max(uint64(1), task.Progress.NextSequence); ; {
		if iterations >= taskmodel.MaxIterations {
			return taskResult(fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "model iteration limit exceeded"))
		}
		request := buildModelRequest(model.ModelRequest{
			TaskID:          task.ID,
			PromptCacheKey:  task.SessionID.String(),
			Model:           task.Config.Model,
			MaxOutputTokens: taskModel.MaxOutputTokens,
		}, modelContext, r.Tools)
		toolPayload, err := json.Marshal(request.Tools)
		if err != nil {
			return taskResult(failCause(contracts.TaskFailureContract, err))
		}
		compressed, changed, err := r.CompressionStrategy.Compress(ctx, modelContext, contextcompress.Options{
			SummaryFormat:   r.SummaryFormat,
			ContextWindow:   task.Config.Model.ContextWindow,
			MaxOutputTokens: taskModel.MaxOutputTokens,
			ReservedTokens:  appcontext.EstimateTokens(string(toolPayload)),
		}, func(summaryCtx context.Context, history appcontext.ModelContext, limit int) (string, error) {
			current := sequence
			sequence++
			return r.summarizeContext(summaryCtx, task, taskModel.Stream, history, limit, current)
		})
		if err != nil {
			err = modelFailure(err)
			r.emitError(task.AgentID, task.ID, "", err)
			return taskResult(err)
		}
		if changed {
			r.Logf("context compressed task=%s entriesBefore=%d entriesAfter=%d", task.ID, len(modelContext.Entries), len(compressed.Entries))
			modelContext = compressed
			request.Context = modelContext.Clone()
		}
		turnSequence := sequence
		if err := r.Progress.RecordIteration(ctx, task.ID, turnSequence); err != nil {
			if errors.Is(err, contracts.New(contracts.TaskResourceLimit, "")) {
				return taskResult(failCause(contracts.TaskFailureResourceLimit, err))
			}
			return taskResult(failCause(contracts.TaskFailureStorage, err))
		}
		sequence++
		iterations++
		turnID := contracts.NewTurnID(task.ID, turnSequence)
		request.TurnID = turnID
		outcome, failureCode, finished, err := r.executeTurn(ctx, task, taskModel, messageWriter, request, &modelContext, turnID)

		if err != nil || outcome != taskmodel.TaskCompleted || finished {
			return outcome, failureCode, err
		}
		// 工具的独立风险审核也会分配请求序号，下一轮不能复用本地旧序号。
		stored, err := r.Progress.LoadTask(ctx, task.ID)
		if err != nil {
			return taskResult(failCause(contracts.TaskFailureStorage, err))
		}
		sequence = max(sequence, stored.Progress.NextSequence)
	}
}

// executeTurn 执行一轮模型、工具和回执业务，返回明确的继续/完成决策。
func (r *Runner) executeTurn(
	ctx context.Context,
	task taskmodel.Task,
	taskModel model.Model,
	messageWriter repository.MessageWriter,
	request model.ModelRequest,
	modelContext *appcontext.ModelContext,
	turnID contracts.TurnID,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, bool, error) {
	r.emit(Event{
		Kind:      EventTurnStarted,
		SessionID: task.SessionID,
		AgentID:   task.AgentID,
		TaskID:    task.ID,
		TurnID:    turnID,
	})
	r.Logf("provider request prepared id=%s model=%s contextEntries=%d turn=%s", task.ID, task.Config.Model.ModelID, len(modelContext.Entries), turnID)
	r.emit(Event{
		Kind:      EventProviderWaiting,
		SessionID: task.SessionID,
		AgentID:   task.AgentID,
		TaskID:    task.ID,
		TurnID:    turnID,
		Name:      task.Config.Model.ProviderID + "/" + task.Config.Model.ModelID,
	})
	response, err := requestModel(ctx, taskModel.Stream, request, hooks.Identity{
		SessionID: task.SessionID,
		AgentID:   task.AgentID,
		TaskID:    task.ID,
		TurnID:    turnID,
	}, r.HookDispatcher, func(event model.ModelStreamEvent) {
		r.emitModelEvent(task.SessionID, task.AgentID, task.ID, turnID, event)
	})
	if err != nil {
		err = modelFailure(err)
		r.emitError(task.AgentID, task.ID, turnID, err)
		outcome, code, cause := taskResult(err)
		return outcome, code, false, cause
	}
	if err := r.Progress.RecordModelOutput(ctx, task.ID, response.textBytes); err != nil {
		outcome, code, cause := taskResult(failCause(contracts.TaskFailureStorage, err))
		return outcome, code, false, cause
	}
	assistant := model.ContextBlocksFromEvents(response.events)
	calls := response.toolCalls()
	metadata := contracts.ToolExecutionMetadata{
		TaskID:        task.ID,
		TurnID:        turnID,
		SessionID:     task.SessionID,
		AgentID:       task.AgentID,
		WorkspacePath: task.Config.WorkspacePath,
	}
	{
		message := taskMessage(sessionmodel.MessageData{
			ID:         "assistant:" + turnID.String(),
			RequestID:  task.RequestID.String(),
			TaskID:     task.ID.String(),
			TurnID:     turnID.String(),
			StopReason: string(response.stopReason),
			Role:       sessionmodel.RoleAssistant,
			AuthorKind: sessionmodel.AuthorAgent,
			AuthorID:   task.AgentID.String(),
		}, assistant)
		value := repository.AssistantWrite{Message: message}
		for _, call := range calls {
			invocation := toolmodel.NewExecution(contracts.ToolExecutionID(r.IDs.New("toolexecution")), task.ID, turnID, task.SessionID, task.AgentID, call.ID, call.Name, call.Arguments, toolmodel.ArgumentsDigest(call.Name, call.Arguments), r.Clock.Now())
			value.ToolCalls = append(value.ToolCalls, invocation)
		}
		if response.stopReason == model.StopCompleted || response.stopReason == model.StopSequence {
			value.FinalOutput = &taskmodel.FinalOutput{AssistantID: message.ID, StopReason: message.StopReason}
		}
		message, inserted, err := messageWriter.AppendAssistant(ctx, value)
		if err != nil {
			r.emitError(task.AgentID, task.ID, turnID, err)
			outcome, code, cause := taskResult(failCause(contracts.TaskFailureStorage, err))
			return outcome, code, false, cause
		}
		if inserted && value.FinalOutput != nil {
			if hookErr := r.HookDispatcher.OnCommitted(context.WithoutCancel(ctx), hooks.CommittedEvent{
				Identity: hooks.Identity{
					SessionID: task.SessionID,
					AgentID:   task.AgentID,
					TaskID:    task.ID,
					TurnID:    turnID,
				},
				Message: &message,
			}); hookErr != nil {
				r.Logf("assistant committed hook failed: %v", hookErr)
			}
		}
	}
	for _, call := range calls {
		r.emit(Event{
			Kind:      EventToolCall,
			SessionID: task.SessionID,
			AgentID:   task.AgentID,
			TaskID:    task.ID,
			TurnID:    turnID,
			CallID:    call.ID,
			Name:      string(call.Name),
			Input:     append([]byte(nil), call.Arguments...),
		})
	}
	*modelContext = modelContext.AppendProviderOutput(assistant)
	if response.stopReason == model.StopCompleted || response.stopReason == model.StopSequence {

		r.emit(Event{
			Kind:       EventTurnCompleted,
			SessionID:  task.SessionID,
			AgentID:    task.AgentID,
			TaskID:     task.ID,
			TurnID:     turnID,
			StopReason: response.stopReason,
		})
		return taskmodel.TaskCompleted, "", true, nil
	}
	results, err := r.runToolCalls(ctx, messageWriter, calls, metadata, task.RequestID)
	if err != nil {
		if errors.Is(err, errAwaitingApproval) {
			return taskmodel.TaskAwaitingApproval, "", false, nil
		}
		r.emitError(task.AgentID, task.ID, turnID, err)
		outcome, code, cause := taskResult(err)
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
		Kind:       EventTurnCompleted,
		SessionID:  task.SessionID,
		AgentID:    task.AgentID,
		TaskID:     task.ID,
		TurnID:     turnID,
		StopReason: response.stopReason,
	})
	return taskmodel.TaskCompleted, "", false, nil
}

func (r *Runner) buildModel(config model.Config) (model.Model, error) {
	built, err := r.ModelBuilder.BuildModel(config)
	if err != nil {
		return model.Model{}, failCause(contracts.TaskFailureProvider, err)
	}
	if built.Stream == nil {
		return model.Model{}, fail(contracts.TaskFailureProviderUnavailable, ErrorProvider, "model stream is unavailable")
	}
	return built, nil
}
