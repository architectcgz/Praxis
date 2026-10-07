package loop

import (
	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	turnmodel "praxis/internal/core/turn"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/agent_runtime"
	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	toolcontracts "praxis/internal/tools/contracts"
)

const (
	defaultMaxSteps     = 32
	defaultMaxToolCalls = 128
)

// Config 是 loop 的执行依赖；由组合根装配，不包含跨次执行的可变状态。
type Config struct {
	ModelBuilder  model.ModelBuilder
	Tools         toolcontracts.ToolCatalog
	ToolCalls     ToolCallHandler
	Turns         TurnRecorder
	EventObserver agentruntime.AgentEventObserver
	Logf          func(string, ...any)
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
		r.emitError(task, "", err)
		return taskResult(err)
	}

	budget := newTaskBudget(permissions.ResourceLimits)
	for sequence := 1; sequence <= budget.maxSteps; sequence++ {
		// 每次迭代必须先在日志中留下 running 迭代，恢复才能对账未结算的调用。
		turn, err := r.Turns.RecordStart(ctx, TurnParams{
			TaskID:    task.ID,
			SessionID: task.SessionID,
			AgentID:   task.AgentID,
			Sequence:  uint64(sequence),
		})
		if err != nil {
			r.emitError(task, "", err)
			return taskResult(failCause(contracts.TaskFailureStorage, fmt.Errorf("record turn start: %w", err)))
		}
		r.emit(agentruntime.AgentEvent{
			Kind:    agentruntime.AgentEventTurnStarted,
			AgentID: task.AgentID,
			TaskID:  task.ID,
			TurnID:  turn.ID,
		})
		assistant, calls, err := r.runStep(ctx, task, taskModel, modelContext, budget, turn)
		if err != nil {
			r.emitError(task, turn.ID, err)
			return r.settleTurnErr(ctx, turn, err)
		}
		for _, call := range calls {
			if call.ID == "" || !call.Name.Valid() || len(call.Arguments) > 0 && !json.Valid(call.Arguments) {
				err := fail(contracts.TaskFailureContract, ErrorContract, "provider emitted an invalid tool call")
				r.emitError(task, turn.ID, err)
				return r.settleTurnErr(ctx, turn, err)
			}
		}
		if len(assistant) > 0 {
			if err := appendAssistantMessage(ctx, messageRecorder, task, turn.ID, assistant); err != nil {
				r.emitError(task, turn.ID, err)
				return r.settleTurnErr(ctx, turn, err)
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
		modelContext = appendProviderOutput(modelContext, assistant)
		if len(calls) == 0 {
			return r.settleTurn(ctx, turn, taskmodel.TaskCompleted, "", nil)
		}
		modelContext, err = r.runToolCalls(ctx, messageRecorder, task, permissions, modelContext, calls, budget, turn.ID)
		if err != nil {
			r.emitError(task, turn.ID, err)
			return r.settleTurnErr(ctx, turn, err)
		}
		// 本次迭代已完整执行（含工具调用）；先结算迭代再进入下一次模型请求。
		if outcome, code, settleErr := r.settleTurn(ctx, turn, taskmodel.TaskCompleted, "", nil); settleErr != nil {
			r.emitError(task, turn.ID, settleErr)
			return outcome, code, settleErr
		}
	}
	err = fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "task step limit exceeded")
	r.emitError(task, "", err)
	return taskResult(err)
}

// settleTurnErr 按 loop 的失败分类结算当前迭代，再返回 loop 结果。
func (r *Runner) settleTurnErr(
	ctx context.Context,
	turn turnmodel.Turn,
	err error,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	outcome, code, cause := taskResult(err)
	return r.settleTurn(ctx, turn, outcome, code, cause)
}

// settleTurn 结算当前迭代后返回 loop 结果；迭代必须落库，否则恢复无法对账。
// 结算脱离已取消的 Context，避免被中断的迭代永久停留在 running。
func (r *Runner) settleTurn(
	ctx context.Context,
	turn turnmodel.Turn,
	outcome taskmodel.TaskOutcome,
	code contracts.TaskFailureCode,
	err error,
) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	turnOutcome, failureCode, message := turnSettlement(ctx, outcome, code, err)
	if endErr := r.Turns.RecordEnd(context.WithoutCancel(ctx), turn.ID, turnOutcome, failureCode, message); endErr != nil {
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
		return model.Model{}, fail(contracts.TaskFailureProviderUnavailable, ErrorProvider, "task model stream is unavailable")
	}
	return built, nil
}

// runStep 发起一次模型请求，按事件顺序返回 assistant blocks 和待派发工具调用。
func (r *Runner) runStep(
	ctx context.Context,
	task taskmodel.Task,
	taskModel model.Model,
	modelContext appcontext.ModelContext,
	budget *taskBudget,
	turn turnmodel.Turn,
) ([]appcontext.ContextBlock, []ToolCall, error) {
	permissions := task.Input.Security.Permissions
	request := buildModelRequest(task, permissions, modelContext, taskModel, r.Tools, turn.ID)
	if err := budget.reserveInput(request); err != nil {
		return nil, nil, err
	}
	r.Logf("provider request prepared id=%s model=%s contextEntries=%d turn=%s", task.ID, task.Input.Model.ModelID, len(modelContext.Entries), turn.ID)
	r.emit(agentruntime.AgentEvent{
		Kind:    agentruntime.AgentEventProviderWaiting,
		AgentID: task.AgentID,
		TaskID:  task.ID,
		TurnID:  turn.ID,
	})
	stream, err := taskModel.Stream.Stream(ctx, request)
	if err != nil {
		// 窗口判定在 provider adapter 里完成（只有它知道本协议序列化出的请求体）。
		var windowErr *ContextWindowExceededError
		if errors.As(err, &windowErr) {
			return nil, nil, fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "model context window exceeded")
		}
		return nil, nil, failCause(contracts.TaskFailureProvider, err)
	}
	var blocks []appcontext.ContextBlock
	text, calls, err := collectStream(ctx, stream, func(event model.ModelStreamEvent) {
		if event.Kind == model.StreamUsage {
			r.emit(agentruntime.AgentEvent{
				Kind:      agentruntime.AgentEventModelUsage,
				SessionID: task.SessionID,
				AgentID:   task.AgentID,
				TaskID:    task.ID,
				TurnID:    turn.ID,
				Usage:     event.Usage,
			})
			return
		}
		if event.Kind == model.StreamToolCall {
			call := event.ToolCall.Snapshot()
			blocks = append(blocks, appcontext.ContextBlock{
				Kind: appcontext.ContextBlockToolCall, CallID: call.ID, Name: string(call.Name), Input: append([]byte(nil), call.Arguments...),
			})
			return
		}
		kind := agentruntime.AgentEventTextDelta
		blockKind := appcontext.ContextBlockText
		if event.Kind == model.StreamThinkingDelta {
			kind = agentruntime.AgentEventThinkingDelta
			blockKind = appcontext.ContextBlockThinking
		}
		if len(blocks) > 0 && blocks[len(blocks)-1].Kind == blockKind {
			blocks[len(blocks)-1].Text += event.Text
		} else {
			blocks = append(blocks, appcontext.ContextBlock{Kind: blockKind, Text: event.Text})
		}
		r.emit(agentruntime.AgentEvent{
			Kind:    kind,
			AgentID: task.AgentID,
			TaskID:  task.ID,
			TurnID:  turn.ID,
			Text:    event.Text,
		})
	})
	if err != nil {
		return nil, nil, failCause(contracts.TaskFailureProvider, err)
	}
	if err := budget.addOutput(len(text)); err != nil {
		return nil, nil, err
	}
	content := blocks[:0]
	for _, block := range blocks {
		if (block.Kind == appcontext.ContextBlockText || block.Kind == appcontext.ContextBlockThinking) && strings.TrimSpace(block.Text) == "" {
			continue
		}
		content = append(content, block)
	}
	return content, calls, nil
}

// runToolCalls 派发当前迭代的全部工具调用。
// 下一次模型请求读取结果前必须写入 durable tool-result receipt，
// 避免进程崩溃后留下无法对账的副作用。
func (r *Runner) runToolCalls(
	ctx context.Context,
	messageRecorder agentruntime.MessageRecorder,
	task taskmodel.Task,
	permissions contracts.TaskPermissions,
	modelContext appcontext.ModelContext,
	calls []ToolCall,
	budget *taskBudget,
	turnID contracts.TurnID,
) (appcontext.ModelContext, error) {
	if err := budget.reserveToolCalls(len(calls)); err != nil {
		return modelContext, err
	}
	for callIndex, rawCall := range calls {
		call := rawCall
		result, err := r.ToolCalls.Invoke(ctx, call, ToolInvocationMetadata{
			TaskID:              task.ID,
			TurnID:              turnID,
			SessionID:           task.SessionID,
			AgentID:             task.AgentID,
			WorkspacePath:       task.Input.WorkspacePath,
			SecurityFingerprint: task.Input.Security.Fingerprint,
		})
		if err != nil {
			// 在结算失败前写入受限回执，确保人工检查时能看到已尝试的副作用。
			block := appcontext.ContextBlock{
				Kind: appcontext.ContextBlockToolResult, CallID: call.ID, Name: string(call.Name),
				Text: "tool execution failed", IsError: true,
			}
			if receiptErr := appendToolResult(ctx, messageRecorder, task, turnID, callIndex, call, block); receiptErr != nil {
				return modelContext, failCause(contracts.TaskFailureStorage,
					&RuntimeError{Code: ErrorStorage, Message: "tool result receipt failed", Cause: receiptErr})
			}
			r.emitToolResult(task, turnID, call, block)
			return modelContext, failCause(contracts.TaskFailureTool,
				&RuntimeError{Code: ErrorTool, Message: "tool execution failed", Cause: err})
		}
		budget.toolCalls++
		resultText := result.Payload
		if resultText == "" {
			resultText = "(empty tool result)"
		}
		if err := budget.addOutput(len(resultText)); err != nil {
			return modelContext, err
		}
		block := appcontext.ContextBlock{
			Kind: appcontext.ContextBlockToolResult, CallID: call.ID, Name: string(call.Name),
			Text: resultText, IsError: result.ErrorClass != "",
		}
		if err := appendToolResult(ctx, messageRecorder, task, turnID, callIndex, call, block); err != nil {
			return modelContext, err
		}
		r.emitToolResult(task, turnID, call, block)
		modelContext = appendToolContextResult(modelContext, call, block)
	}
	return modelContext, nil
}

func buildModelRequest(
	task taskmodel.Task,
	permissions contracts.TaskPermissions,
	modelContext appcontext.ModelContext,
	taskModel model.Model,
	catalog toolcontracts.ToolCatalog,
	turnID contracts.TurnID,
) model.ModelRequest {
	return model.ModelRequest{
		TaskID: task.ID, SessionReference: task.SessionID.String(),
		Context: modelContext.Clone(),
		Model:   task.Input.Model, MaxOutputTokens: taskModel.MaxOutputTokens, Tools: toolDefinitions(permissions, catalog),
		TurnID: turnID,
	}
}

func appendProviderOutput(
	modelContext appcontext.ModelContext,
	blocks []appcontext.ContextBlock,
) appcontext.ModelContext {
	content := make([]appcontext.ContextBlock, 0, len(blocks))
	for _, block := range blocks {
		if block.Kind != appcontext.ContextBlockThinking {
			content = append(content, block.Clone())
		}
	}
	if len(content) == 0 {
		return modelContext
	}
	modelContext.Entries = append(modelContext.Entries, appcontext.ContextEntry{
		Kind:    appcontext.ContextEntryProviderOutput,
		Role:    appcontext.ContextRoleAssistant,
		Content: content,
	})
	return modelContext
}

func appendToolContextResult(
	modelContext appcontext.ModelContext,
	call ToolCall,
	block appcontext.ContextBlock,
) appcontext.ModelContext {
	modelContext.Entries = append(modelContext.Entries, appcontext.ContextEntry{
		Kind: appcontext.ContextEntryToolResult,
		Role: appcontext.ContextRoleTool,
		Content: []appcontext.ContextBlock{{
			Kind:    appcontext.ContextBlockToolResult,
			Text:    block.Text,
			CallID:  call.ID,
			Name:    string(call.Name),
			IsError: block.IsError,
		}},
	})
	return modelContext
}

func appendAssistantMessage(
	ctx context.Context,
	messageRecorder agentruntime.MessageRecorder,
	task taskmodel.Task,
	turnID contracts.TurnID,
	blocks []appcontext.ContextBlock,
) error {
	return appendTaskMessage(ctx, messageRecorder, task, "assistant:"+turnID.String(), sessionmodel.RoleAssistant, sessionmodel.AuthorAgent, string(task.AgentID), blocks)
}

func appendToolResult(
	ctx context.Context,
	messageRecorder agentruntime.MessageRecorder,
	task taskmodel.Task,
	turnID contracts.TurnID,
	callIndex int,
	call ToolCall,
	block appcontext.ContextBlock,
) error {
	return appendTaskMessage(ctx, messageRecorder, task,
		fmt.Sprintf("tool-result:%s:%d:%s", turnID, callIndex, call.ID),
		sessionmodel.RoleTool, sessionmodel.AuthorTool, string(call.Name), []appcontext.ContextBlock{block})
}

func appendTaskMessage(
	ctx context.Context,
	messageRecorder agentruntime.MessageRecorder,
	task taskmodel.Task,
	id string,
	role sessionmodel.Role,
	authorKind sessionmodel.AuthorKind,
	authorID string,
	blocks []appcontext.ContextBlock,
) error {
	value := sessionmodel.MessageData{
		ID: id, RequestID: task.RequestID.String(), TaskID: task.ID.String(),
		Role: role, AuthorKind: authorKind, AuthorID: authorID,
		Blocks: make([]sessionmodel.Block, len(blocks)),
	}
	for index, block := range blocks {
		value.Blocks[index] = sessionmodel.Block{
			Kind: sessionmodel.BlockKind(block.Kind), Text: block.Text, CallID: block.CallID,
			Name: block.Name, Input: append([]byte(nil), block.Input...), IsError: block.IsError,
		}
	}
	_, err := messageRecorder.Append(ctx, value)
	return err
}

// taskBudget 执行单次 task 的资源限制。
// 输出字节数跨迭代的模型输出和工具结果累计，工具计数仅在调用成功后递增。
type taskBudget struct {
	limits       contracts.ResourceLimits
	maxSteps     int
	maxToolCalls int
	outputBytes  int64
	toolCalls    int
}

func newTaskBudget(limits contracts.ResourceLimits) *taskBudget {
	budget := &taskBudget{limits: limits, maxSteps: limits.MaxSteps, maxToolCalls: limits.MaxToolCalls}
	if budget.maxSteps <= 0 {
		budget.maxSteps = defaultMaxSteps
	}
	if budget.maxToolCalls <= 0 {
		budget.maxToolCalls = defaultMaxToolCalls
	}
	return budget
}

func (b *taskBudget) reserveInput(request model.ModelRequest) error {
	if b.limits.MaxInputBytes > 0 && stepInputBytes(request) > b.limits.MaxInputBytes {
		return fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "task input limit exceeded")
	}
	return nil
}

func (b *taskBudget) addOutput(bytes int) error {
	b.outputBytes += int64(bytes)
	if b.limits.MaxOutputBytes > 0 && b.outputBytes > b.limits.MaxOutputBytes {
		return fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "task output limit exceeded")
	}
	return nil
}

func (b *taskBudget) reserveToolCalls(count int) error {
	if b.toolCalls+count > b.maxToolCalls {
		return fail(contracts.TaskFailureResourceLimit, ErrorResourceLimit, "task tool call limit exceeded")
	}
	return nil
}

// taskFailure 携带引擎错误对应的持久化结束结果。
// taskResult 在统一的 run 边界把它转换为返回值。
type taskFailure struct {
	outcome taskmodel.TaskOutcome
	code    contracts.TaskFailureCode
	cause   error
}

func (f *taskFailure) Error() string {
	if f.cause == nil {
		return string(f.code)
	}
	return f.cause.Error()
}

func (f *taskFailure) Unwrap() error { return f.cause }

func fail(code contracts.TaskFailureCode, runtimeCode ErrorCode, message string) error {
	return &taskFailure{
		outcome: taskmodel.TaskFailed,
		code:    code,
		cause:   &RuntimeError{Code: runtimeCode, Message: message},
	}
}

func failCause(code contracts.TaskFailureCode, cause error) error {
	return &taskFailure{outcome: taskmodel.TaskFailed, code: code, cause: cause}
}

func taskResult(err error) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	var failure *taskFailure
	if errors.As(err, &failure) {
		return failure.outcome, failure.code, failure.cause
	}
	return failedTask(err)
}

func toolDefinitions(permissions contracts.TaskPermissions, catalog toolcontracts.ToolCatalog) []ToolDefinition {
	definitions := make([]ToolDefinition, 0, len(permissions.AllowedTools))
	for _, definition := range catalog.List() {
		if permissions.AllowsTool(definition.Name) {
			definitions = append(definitions, definition.Snapshot())
		}
	}
	return definitions
}

func collectStream(ctx context.Context, stream <-chan model.ModelStreamEvent, onEvent func(model.ModelStreamEvent)) (string, []ToolCall, error) {
	var text strings.Builder
	calls := make([]ToolCall, 0)
	for {
		select {
		case <-ctx.Done():
			return text.String(), calls, ctx.Err()
		case event, ok := <-stream:
			if !ok {
				return text.String(), calls, nil
			}
			switch event.Kind {
			case model.StreamTextDelta:
				text.WriteString(event.Text)
				if onEvent != nil && event.Text != "" {
					onEvent(event)
				}
			case model.StreamThinkingDelta:
				if onEvent != nil && event.Text != "" {
					onEvent(event)
				}
			case model.StreamToolCall:
				event.ToolCall = event.ToolCall.Snapshot()
				calls = append(calls, event.ToolCall)
				if onEvent != nil {
					onEvent(event)
				}
			case model.StreamUsage:
				if event.Usage != nil && event.Usage.Valid() && onEvent != nil {
					onEvent(event)
				}
			case model.StreamComplete:
				return text.String(), calls, nil
			case model.StreamError:
				if event.Err != nil {
					return text.String(), calls, event.Err
				}
				return text.String(), calls, errors.New("provider stream failed")
			default:
				return text.String(), calls, errors.New("provider stream emitted an unknown event")
			}
		}
	}
}

func stepInputBytes(request model.ModelRequest) int64 {
	var total int64
	total += int64(len([]byte(request.Context.SystemPrompt)))
	for _, entry := range request.Context.Entries {
		for _, block := range entry.Content {
			total += int64(len([]byte(block.Text)))
			total += int64(len(block.Input))
		}
	}
	return total
}

func failedTask(err error) (taskmodel.TaskOutcome, contracts.TaskFailureCode, error) {
	if err == nil {
		err = errors.New("loop failed")
	}
	return taskmodel.TaskFailed, contracts.TaskFailureRuntimeFailed, fmt.Errorf("loop: %w", err)
}
