package loop

import (
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/hooks"
	toolcontracts "praxis/internal/tools/contracts"

	"context"
	"encoding/json"
	"errors"
)

// Config 是 loop 的执行依赖；由组合根装配，不包含跨次执行的可变状态。
type Config struct {
	Progress       TaskProgress
	ModelBuilder   model.ModelBuilder
	Tools          toolcontracts.ToolCatalog
	ToolExecutor   ToolExecutor
	HookDispatcher *hooks.Dispatcher
	// CompressionStrategy 为空时使用完整远程压缩；其他策略通过依赖注入替换。
	CompressionStrategy contextcompress.Strategy
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
	return Runner{Config: config}, nil
}

// Run 使用本次执行输入与消息记录器运行 provider/tool loop，返回结果及失败分类。
// 业务上层提供已选通用内容与续接消息，消息记录器负责可靠提交输出；
// TaskProgress 负责加载持久化预算、记录模型及摘要请求和模型输出消耗；
// 每次执行的输入（context、消息记录器）为空时返回错误；
// 持久化生命周期、取消和后续任务调度由执行协调器负责。
func (r *Runner) Run(ctx context.Context, task taskmodel.Task, content appcontext.Content, continuation []sessionmodel.MessageData, messageRecorder MessageRecorder) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	if ctx == nil {
		return failedTask(errors.New("loop context is required"))
	}
	if messageRecorder == nil {
		return failedTask(errors.New("loop message recorder is required"))
	}
	return r.run(ctx, task, content, continuation, messageRecorder)
}

// run 是 task 的迭代 loop。每轮只发起一次模型请求；确认自然完成时结束，
// 否则必须先持久化每个工具调用结果，再构造下一次模型请求。
func (r *Runner) run(
	ctx context.Context,
	task taskmodel.Task,
	content appcontext.Content,
	continuation []sessionmodel.MessageData,
	messageRecorder MessageRecorder,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	task, err := r.Progress.LoadTask(ctx, task.ID)
	if err != nil {
		return taskResult(failCause(contracts.TaskFailureStorage, err))
	}
	limits := task.Config.ResourceLimits
	modelContext, err := (appcontext.ContextBuilder{}).Build(content, appcontext.Limits{ContextWindow: task.Config.Model.ContextWindow, MaxOutputTokens: task.Config.Model.MaxOutputTokens})
	if err != nil {
		return taskResult(modelFailure(err))
	}
	budget := newTaskBudget(limits)
	budget.outputBytes = task.Progress.OutputBytes
	if limits.MaxOutputBytes > 0 && budget.outputBytes > limits.MaxOutputBytes {
		return taskResult(fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "task output limit exceeded"))
	}
	if task.Progress.FinalStopReason != "" {
		return taskmodel.TaskCompleted, "", nil
	}
	if err := r.resumeToolCalls(ctx, task, continuation, messageRecorder, &modelContext, budget); err != nil {
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

	for sequence := max(uint64(1), task.Progress.NextSequence); ; {
		request := buildModelRequest(model.ModelRequest{
			TaskID:           task.ID,
			SessionReference: task.SessionID.String(),
			Model:            task.Config.Model,
			MaxOutputTokens:  taskModel.MaxOutputTokens,
		}, modelContext, r.Tools)
		toolPayload, err := json.Marshal(request.Tools)
		if err != nil {
			return taskResult(failCause(contracts.TaskFailureContract, err))
		}
		compressed, changed, err := r.CompressionStrategy.Compress(ctx, modelContext, contextcompress.Options{
			ContextWindow:   task.Config.Model.ContextWindow,
			MaxOutputTokens: taskModel.MaxOutputTokens,
			ReservedTokens:  appcontext.EstimateTokens(string(toolPayload)),
		}, func(summaryCtx context.Context, history appcontext.ModelContext, limit int) (string, error) {
			current := sequence
			sequence++
			return r.summarizeContext(summaryCtx, task, taskModel.Stream, history, limit, current, budget)
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
		if err := r.Progress.RecordModelRequest(ctx, task.ID, turnSequence); err != nil {
			return taskResult(failCause(contracts.TaskFailureStorage, err))
		}
		sequence++
		turnID := contracts.NewTurnID(task.ID, turnSequence)
		request.TurnID = turnID
		outcome, failureCode, finished, err := r.executeTurn(ctx, task, taskModel, messageRecorder, request, &modelContext, budget, turnID)

		if err != nil || outcome != taskmodel.TaskCompleted || finished {
			return outcome, failureCode, err
		}
	}
}

// executeTurn 执行一轮模型、工具和回执业务，返回明确的继续/完成决策。
func (r *Runner) executeTurn(
	ctx context.Context,
	task taskmodel.Task,
	taskModel model.Model,
	messageRecorder MessageRecorder,
	request model.ModelRequest,
	modelContext *appcontext.ModelContext,
	budget *taskBudget,
	turnID contracts.TurnID,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, bool, error) {
	r.emit(Event{
		Kind:      EventTurnStarted,
		SessionID: task.SessionID,
		AgentID:   task.AgentID,
		TaskID:    task.ID,
		TurnID:    turnID,
	})
	if err := budget.reserveInput(request); err != nil {
		r.emitError(task.AgentID, task.ID, turnID, err)
		outcome, code, cause := taskResult(err)
		return outcome, code, false, cause
	}
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
	outputErr := budget.addOutput(response.textBytes)
	if err := r.Progress.RecordModelOutput(ctx, task.ID, response.textBytes); err != nil {
		outcome, code, cause := taskResult(failCause(contracts.TaskFailureStorage, err))
		return outcome, code, false, cause
	}
	assistant := model.ContextBlocksFromEvents(response.events)
	calls := response.toolCalls()
	metadata := contracts.ToolInvocationContext{
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
		message, inserted, err := messageRecorder.AppendAssistant(ctx, message, metadata)
		if err != nil {
			r.emitError(task.AgentID, task.ID, turnID, err)
			outcome, code, cause := taskResult(failCause(contracts.TaskFailureStorage, err))
			return outcome, code, false, cause
		}
		if inserted && (response.stopReason == model.StopCompleted || response.stopReason == model.StopSequence) {
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
	if outputErr != nil {
		outcome, code, cause := taskResult(outputErr)
		return outcome, code, false, cause
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
	results, err := r.runToolCalls(ctx, messageRecorder, calls, metadata, task.RequestID, budget)
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
