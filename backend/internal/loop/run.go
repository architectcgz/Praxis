package loop

import (
	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	contextcompress "praxis/internal/core/context/compress"
	"praxis/internal/core/model"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	turnmodel "praxis/internal/core/turn"
	toolcontracts "praxis/internal/tools/contracts"

	"context"
	"encoding/json"
	"errors"
	"fmt"

	"praxis/internal/agent_runtime"
)

// Config 是 loop 的执行依赖；由组合根装配，不包含跨次执行的可变状态。
type Config struct {
	ModelBuilder model.ModelBuilder
	Tools        toolcontracts.ToolCatalog
	ToolCalls    ToolCallHandler
	Turns        TurnRecorder
	// CompressionStrategy 为空时使用完整远程压缩；其他策略通过依赖注入替换。
	CompressionStrategy contextcompress.Strategy
	EventObserver       agentruntime.AgentEventObserver
	Logf                func(string, ...any)
}

// Runner 是持有已校验依赖的 loop 执行器，可跨多次 Task 执行复用。
type Runner struct {
	Config
}

// NewRunner 校验并冻结 loop 的执行依赖。缺少任一必要依赖时直接返回错误，
// 使装配失败的执行器无法进入调度，而不是在每次执行时才发现。
func NewRunner(config Config) (Runner, error) {
	if config.ModelBuilder == nil {
		return Runner{}, errors.New("loop model builder is required")
	}
	if config.Tools == nil {
		return Runner{}, errors.New("loop tool catalog is required")
	}
	if config.ToolCalls == nil {
		return Runner{}, errors.New("loop tool call handler is required")
	}
	if config.Turns == nil {
		return Runner{}, errors.New("loop turn recorder is required")
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

// Run 使用本次执行快照与消息记录器运行 provider/tool loop，返回结果及失败分类。
// 消息记录器只作为持久化回执的写入端（会话历史不会回读）；
// 每次执行的输入（context、消息记录器）为空时返回错误；
// 持久化生命周期、取消和后续任务调度由 runtime 负责。
func (r *Runner) Run(ctx context.Context, task taskmodel.Task, messageRecorder agentruntime.MessageRecorder) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	if ctx == nil {
		return failedTask(errors.New("loop context is required"))
	}
	if messageRecorder == nil {
		return failedTask(errors.New("loop message recorder is required"))
	}
	return r.run(ctx, task, messageRecorder)
}

// run 是 task 的迭代 loop。每轮只发起一次模型请求；没有工具调用时结束，
// 否则必须先持久化每个工具调用结果，再构造下一次模型请求。
func (r *Runner) run(
	ctx context.Context,
	task taskmodel.Task,
	messageRecorder agentruntime.MessageRecorder,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	permissions := task.Input.Security.Permissions
	// 会话历史已在任务创建时冻结进快照；本任务新增的上下文条目在循环内内存累加，
	// 落库消息只作为回执写入，不回读，因此这里不需要读取会话历史。
	modelContext := task.Input.Context.Clone()
	taskModel, err := r.buildModel(task.Input.Model)
	if err != nil {
		r.emitError(task.AgentID, task.ID, "", err)
		return taskResult(err)
	}

	budget := newTaskBudget(permissions.ResourceLimits)
	for sequence := uint64(1); ; {
		request := buildModelRequest(model.ModelRequest{
			TaskID:           task.ID,
			SessionReference: task.SessionID.String(),
			Model:            task.Input.Model,
			MaxOutputTokens:  taskModel.MaxOutputTokens,
		}, modelContext, permissions, r.Tools)
		toolPayload, err := json.Marshal(request.Tools)
		if err != nil {
			return taskResult(failCause(contracts.TaskFailureContract, err))
		}
		compressed, changed, err := r.CompressionStrategy.Compress(ctx, modelContext, contextcompress.Options{
			ContextWindow:   task.Input.Model.ContextWindow,
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
		// 每次迭代必须先在日志中留下 running 迭代，恢复才能对账未结算的调用。
		turn, err := r.Turns.RecordStart(ctx, TurnParams{
			TaskID:    task.ID,
			SessionID: task.SessionID,
			AgentID:   task.AgentID,
			Sequence:  sequence,
		})
		if err != nil {
			r.emitError(task.AgentID, task.ID, "", err)
			return taskResult(failCause(contracts.TaskFailureStorage, fmt.Errorf("record turn start: %w", err)))
		}
		sequence++
		r.emit(agentruntime.AgentEvent{
			Kind:    agentruntime.AgentEventTurnStarted,
			AgentID: task.AgentID,
			TaskID:  task.ID,
			TurnID:  turn.ID,
		})
		request.TurnID = turn.ID
		if err := budget.reserveInput(request); err != nil {
			r.emitError(task.AgentID, task.ID, turn.ID, err)
			return r.settleTurnErr(ctx, turn.ID, err)
		}
		r.Logf("provider request prepared id=%s model=%s contextEntries=%d turn=%s", task.ID, task.Input.Model.ModelID, len(modelContext.Entries), turn.ID)
		r.emit(agentruntime.AgentEvent{
			Kind:    agentruntime.AgentEventProviderWaiting,
			AgentID: task.AgentID,
			TaskID:  task.ID,
			TurnID:  turn.ID,
		})
		response, err := requestModel(ctx, taskModel.Stream, request, func(event model.ModelStreamEvent) {
			r.emitModelEvent(task.SessionID, task.AgentID, task.ID, turn.ID, event)
		})
		if err != nil {
			err = modelFailure(err)
			r.emitError(task.AgentID, task.ID, turn.ID, err)
			return r.settleTurnErr(ctx, turn.ID, err)
		}
		if err := budget.addOutput(response.textBytes); err != nil {
			r.emitError(task.AgentID, task.ID, turn.ID, err)
			return r.settleTurnErr(ctx, turn.ID, err)
		}
		assistant := model.ContextBlocksFromEvents(response.events)
		calls := response.toolCalls()
		if err := validateToolCalls(calls); err != nil {
			r.emitError(task.AgentID, task.ID, turn.ID, err)
			return r.settleTurnErr(ctx, turn.ID, err)
		}
		metadata := ToolInvocationMetadata{
			TaskID:              task.ID,
			TurnID:              turn.ID,
			SessionID:           task.SessionID,
			AgentID:             task.AgentID,
			WorkspacePath:       task.Input.WorkspacePath,
			SecurityFingerprint: task.Input.Security.Fingerprint,
		}
		if len(assistant) > 0 {
			message := taskMessage(sessionmodel.MessageData{
				ID:         "assistant:" + turn.ID.String(),
				RequestID:  task.RequestID.String(),
				TaskID:     task.ID.String(),
				Role:       sessionmodel.RoleAssistant,
				AuthorKind: sessionmodel.AuthorAgent,
				AuthorID:   task.AgentID.String(),
			}, assistant)
			if err := r.ToolCalls.RecordAssistant(ctx, messageRecorder, message, metadata); err != nil {
				r.emitError(task.AgentID, task.ID, turn.ID, err)
				return r.settleTurnErr(ctx, turn.ID, err)
			}
		}
		for _, call := range calls {
			r.emit(agentruntime.AgentEvent{
				Kind:    agentruntime.AgentEventToolCall,
				AgentID: task.AgentID,
				TaskID:  task.ID,
				TurnID:  turn.ID,
				CallID:  call.ID,
				Name:    string(call.Name),
				Input:   append([]byte(nil), call.Arguments...),
			})
		}
		r.emit(agentruntime.AgentEvent{
			Kind:    agentruntime.AgentEventTurnCompleted,
			AgentID: task.AgentID,
			TaskID:  task.ID,
			TurnID:  turn.ID,
		})
		modelContext = modelContext.AppendProviderOutput(assistant)
		if len(calls) == 0 {
			return r.settleTurn(ctx, turn.ID, taskmodel.TaskCompleted, "", nil)
		}
		results, err := r.runToolCalls(ctx, messageRecorder, calls, metadata, task.RequestID, budget)
		if err != nil {
			r.emitError(task.AgentID, task.ID, turn.ID, err)
			return r.settleTurnErr(ctx, turn.ID, err)
		}
		for index, result := range results {
			modelContext = modelContext.AppendToolResult(appcontext.NewToolResultBlock(
				calls[index].ID,
				string(calls[index].Name),
				result.Payload,
				result.ErrorClass != "",
			))
		}
		// 本次迭代已完整执行（含工具调用）；先结算迭代再进入下一次模型请求。
		if outcome, code, settleErr := r.settleTurn(ctx, turn.ID, taskmodel.TaskCompleted, "", nil); settleErr != nil {
			r.emitError(task.AgentID, task.ID, turn.ID, settleErr)
			return outcome, code, settleErr
		}
	}
}

// settleTurnErr 按 loop 的失败分类结算当前迭代，再返回 loop 结果。
func (r *Runner) settleTurnErr(
	ctx context.Context,
	turnID contracts.TurnID,
	err error,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	outcome, code, cause := taskResult(err)
	return r.settleTurn(ctx, turnID, outcome, code, cause)
}

// settleTurn 结算当前迭代后返回 loop 结果；迭代必须落库，否则恢复无法对账。
// 结算脱离已取消的 Context，避免被中断的迭代永久停留在 running。
func (r *Runner) settleTurn(
	ctx context.Context,
	turnID contracts.TurnID,
	outcome taskmodel.TaskOutcome,
	code contracts.TaskFailureCode,
	err error,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	turnOutcome, failureCode, message := turnSettlement(ctx, outcome, code, err)
	if endErr := r.Turns.RecordEnd(context.WithoutCancel(ctx), turnID, turnOutcome, failureCode, message); endErr != nil {
		storageErr := failCause(contracts.TaskFailureStorage, fmt.Errorf("record turn end: %w", endErr))
		if err == nil {
			return taskResult(storageErr)
		}
		return failedTask(errors.Join(err, storageErr))
	}
	return outcome, code, err
}

// turnSettlement 把 loop 结果映射为迭代结果；取消优先于普通失败。
func turnSettlement(
	ctx context.Context,
	outcome taskmodel.TaskOutcome,
	code contracts.TaskFailureCode,
	err error,
) (turnmodel.TurnOutcome, contracts.TaskFailureCode, string) {
	if ctx.Err() != nil {
		if code == "" {
			code = contracts.TaskFailureRuntimeCancelled
		}
		return turnmodel.TurnInterrupted, code, ""
	}
	if outcome == taskmodel.TaskCompleted {
		return turnmodel.TurnCompleted, "", ""
	}
	if code == "" {
		code = contracts.TaskFailureRuntimeFailed
	}
	return turnmodel.TurnFailed, code, contracts.TaskFailureMessage(code, err)
}

func (r *Runner) buildModel(snapshot model.ModelSnapshot) (model.Model, error) {
	built, err := r.ModelBuilder.BuildModel(snapshot)
	if err != nil {
		return model.Model{}, failCause(contracts.TaskFailureProvider, err)
	}
	if built.Stream == nil {
		return model.Model{}, fail(contracts.TaskFailureProviderUnavailable, ErrorProvider, "model stream is unavailable")
	}
	return built, nil
}
