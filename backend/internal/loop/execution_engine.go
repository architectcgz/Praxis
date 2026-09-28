package loop

import (
	"praxis/internal/contracts"
	executionmodel "praxis/internal/execution"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	appcontext "praxis/internal/context"
	"praxis/internal/runtime/agent"

	runtimecontract "praxis/internal/runtime"
)

const (
	defaultMaxTurns     = 32
	defaultMaxToolCalls = 128
)

// ExecutionModelBuilder 按已冻结的 execution 模型快照构建一次运行时模型。
type ExecutionModelBuilder interface {
	BuildExecutionModel(contracts.ExecutionModelSnapshot) (ExecutionModel, error)
}

type ExecutionEngineConfig struct {
	ModelBuilder  ExecutionModelBuilder
	Tools         ToolCatalog
	ToolCalls     ToolCallHandler
	EventObserver agent.AgentEventObserver
	Logf          func(string, ...any)
}

// ExecutionEngine 运行一个已持久化的 AgentExecution，直到得到结算结果。
// 它只负责 model/tool loop；生命周期 receipt 和产品 settlement 仍由 Agent runtime
// 与 settlement application service 负责。
type ExecutionEngine struct {
	modelBuilder  ExecutionModelBuilder
	tools         ToolCatalog
	toolCalls     ToolCallHandler
	eventObserver agent.AgentEventObserver
	logf          func(string, ...any)
}

func NewExecutionEngine(config ExecutionEngineConfig) (*ExecutionEngine, error) {
	if config.ModelBuilder == nil {
		return nil, errors.New("execution model builder is required")
	}
	logf := config.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &ExecutionEngine{
		modelBuilder:  config.ModelBuilder,
		tools:         config.Tools,
		toolCalls:     config.ToolCalls,
		eventObserver: config.EventObserver,
		logf:          logf,
	}, nil
}

// RunWithSession 基于 Agent 自有 transcript 执行一次 execution，
// 并返回持久化 settlement receipt 所需的结果。
func (e *ExecutionEngine) RunWithSession(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	transcript runtimecontract.TranscriptStore,
) (executionmodel.ExecutionOutcome, contracts.ExecutionFailureCode, error) {
	if transcript == nil {
		return failedExecution(errors.New("transcript store is required"))
	}
	return e.run(ctx, execution, transcript)
}

// run 是 execution turn loop。每轮只发起一次模型请求；没有工具调用时结束，
// 否则必须先持久化每个工具调用结果，再构造下一次模型请求。
func (e *ExecutionEngine) run(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	transcript runtimecontract.TranscriptStore,
) (executionmodel.ExecutionOutcome, contracts.ExecutionFailureCode, error) {
	if ctx == nil {
		return failedExecution(errors.New("execution engine context is required"))
	}
	permissions := execution.Input.Security.Permissions
	modelContext, err := initialContext(ctx, execution, transcript)
	if err != nil {
		e.emitError(execution, 0, err)
		return settle(err)
	}
	executionModel, err := e.buildModel(execution.Input.Model)
	if err != nil {
		e.emitError(execution, 0, err)
		return settle(err)
	}

	budget := newExecutionBudget(permissions.ResourceLimits)
	for turn := 1; turn <= budget.maxTurns; turn++ {
		e.emit(agent.AgentEvent{
			Kind:        agent.AgentEventTurnStarted,
			AgentID:     execution.AgentID,
			ExecutionID: execution.ID,
			Turn:        turn,
		})
		assistant, calls, err := e.runTurn(ctx, execution, executionModel, modelContext, budget, turn)
		if err != nil {
			e.emitError(execution, turn, err)
			return settle(err)
		}
		for _, call := range calls {
			if strings.TrimSpace(call.ID) == "" || !call.Name.Valid() || len(call.Input) > 0 && !json.Valid(call.Input) {
				err := fail(contracts.ExecutionFailureContract, ErrorContract, "provider emitted an invalid tool call")
				e.emitError(execution, turn, err)
				return settle(err)
			}
		}
		if len(assistant) > 0 {
			if err := appendAssistantMessage(ctx, transcript, execution, turn, assistant); err != nil {
				e.emitError(execution, turn, err)
				return failedExecution(err)
			}
		}
		for _, call := range calls {
			e.emit(agent.AgentEvent{
				Kind:        agent.AgentEventToolCall,
				AgentID:     execution.AgentID,
				ExecutionID: execution.ID,
				Turn:        turn,
				CallID:      call.ID,
				Name:        string(call.Name),
				Input:       append([]byte(nil), call.Input...),
			})
		}
		e.emit(agent.AgentEvent{
			Kind:        agent.AgentEventTurnCompleted,
			AgentID:     execution.AgentID,
			ExecutionID: execution.ID,
			Turn:        turn,
		})
		modelContext = appendProviderOutput(modelContext, assistant)
		if len(calls) == 0 {
			return executionmodel.ExecutionCompleted, "", nil
		}
		modelContext, err = e.runToolCalls(ctx, transcript, execution, permissions, modelContext, calls, budget, turn)
		if err != nil {
			e.emitError(execution, turn, err)
			return settle(err)
		}
	}
	err = fail(contracts.ExecutionFailureResourceLimit, ErrorResourceLimit, "execution turn limit exceeded")
	e.emitError(execution, budget.maxTurns, err)
	return settle(err)
}

func (e *ExecutionEngine) buildModel(snapshot contracts.ExecutionModelSnapshot) (ExecutionModel, error) {
	built, err := e.modelBuilder.BuildExecutionModel(snapshot)
	if err != nil {
		return ExecutionModel{}, failCause(contracts.ExecutionFailureProvider, err)
	}
	if built.Stream == nil {
		return ExecutionModel{}, fail(contracts.ExecutionFailureProviderUnavailable, ErrorProvider, "execution model stream is unavailable")
	}
	return built, nil
}

// runTurn 发起一次模型请求，按事件顺序返回 assistant blocks 和待派发工具调用。
func (e *ExecutionEngine) runTurn(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	executionModel ExecutionModel,
	modelContext appcontext.ExecutionContext,
	budget *executionBudget,
	turn int,
) ([]appcontext.ContextBlock, []ToolCall, error) {
	permissions := execution.Input.Security.Permissions
	request := buildModelRequest(execution, permissions, modelContext, executionModel, e.tools, turn)
	if err := budget.reserveInput(request); err != nil {
		return nil, nil, err
	}
	e.logf("provider request prepared id=%s model=%s contextEntries=%d turn=%d", execution.ID, execution.Input.Model.ModelID, len(modelContext.Entries), turn)
	stream, err := executionModel.Stream.Stream(ctx, request)
	if err != nil {
		// 窗口判定在 provider adapter 里完成（只有它知道本协议序列化出的请求体）。
		var windowErr *ContextWindowExceededError
		if errors.As(err, &windowErr) {
			return nil, nil, fail(contracts.ExecutionFailureResourceLimit, ErrorResourceLimit, "model context window exceeded")
		}
		return nil, nil, failCause(contracts.ExecutionFailureProvider, err)
	}
	var blocks []appcontext.ContextBlock
	text, calls, err := collectStream(ctx, stream, func(event ModelStreamEvent) {
		if event.Kind == StreamToolCall {
			call := event.ToolCall.Snapshot()
			blocks = append(blocks, appcontext.ContextBlock{
				Kind: appcontext.ContextBlockToolCall, CallID: call.ID, Name: string(call.Name), Input: append([]byte(nil), call.Input...),
			})
			return
		}
		kind := agent.AgentEventTextDelta
		blockKind := appcontext.ContextBlockText
		if event.Kind == StreamThinkingDelta {
			kind = agent.AgentEventThinkingDelta
			blockKind = appcontext.ContextBlockThinking
		}
		if len(blocks) > 0 && blocks[len(blocks)-1].Kind == blockKind {
			blocks[len(blocks)-1].Text += event.Text
		} else {
			blocks = append(blocks, appcontext.ContextBlock{Kind: blockKind, Text: event.Text})
		}
		e.emit(agent.AgentEvent{
			Kind:        kind,
			AgentID:     execution.AgentID,
			ExecutionID: execution.ID,
			Turn:        turn,
			Text:        event.Text,
		})
	})
	if err != nil {
		return nil, nil, failCause(contracts.ExecutionFailureProvider, err)
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

// runToolCalls 派发当前 turn 的全部工具调用。
// 下一次模型请求读取结果前必须写入 durable tool-result receipt，
// 避免进程崩溃后留下无法对账的副作用。
func (e *ExecutionEngine) runToolCalls(
	ctx context.Context,
	transcript runtimecontract.TranscriptStore,
	execution executionmodel.AgentExecution,
	permissions contracts.ExecutionPermissions,
	modelContext appcontext.ExecutionContext,
	calls []ToolCall,
	budget *executionBudget,
	turn int,
) (appcontext.ExecutionContext, error) {
	if e.toolCalls == nil {
		return modelContext, fail(contracts.ExecutionFailureTool, ErrorTool, "tool capability is unavailable")
	}
	if err := budget.reserveToolCalls(len(calls)); err != nil {
		return modelContext, err
	}
	for callIndex, rawCall := range calls {
		call := rawCall.Snapshot()
		if strings.TrimSpace(call.ID) == "" || !call.Name.Valid() || (len(call.Input) > 0 && !json.Valid(call.Input)) {
			return modelContext, fail(contracts.ExecutionFailureContract, ErrorContract, "provider emitted an invalid tool call")
		}
		result, err := e.toolCalls.Invoke(ctx, call, ToolInvocationMetadata{
			ExecutionID:   execution.ID,
			SessionID:     execution.SessionID,
			AgentID:       execution.AgentID,
			WorkspacePath: execution.Input.WorkspacePath,
			Execution:     execution.Input.Runtime,
		})
		if err != nil {
			// 在结算失败前写入受限回执，确保人工检查时能看到已尝试的副作用。
			block := appcontext.ContextBlock{
				Kind: appcontext.ContextBlockToolResult, CallID: call.ID, Name: string(call.Name),
				Text: "tool execution failed", IsError: true,
			}
			if receiptErr := appendToolResult(ctx, transcript, execution, turn, callIndex, call, block); receiptErr != nil {
				return modelContext, failCause(contracts.ExecutionFailureStorage,
					&RuntimeError{Code: ErrorStorage, Message: "tool result receipt failed", Cause: receiptErr})
			}
			e.emitToolResult(execution, turn, call, block)
			return modelContext, failCause(contracts.ExecutionFailureTool,
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
		if err := appendToolResult(ctx, transcript, execution, turn, callIndex, call, block); err != nil {
			return modelContext, err
		}
		e.emitToolResult(execution, turn, call, block)
		modelContext = appendToolContextResult(modelContext, call, block)
	}
	return modelContext, nil
}

func buildModelRequest(
	execution executionmodel.AgentExecution,
	permissions contracts.ExecutionPermissions,
	modelContext appcontext.ExecutionContext,
	executionModel ExecutionModel,
	catalog ToolCatalog,
	turn int,
) ModelRequest {
	return ModelRequest{
		ExecutionID: execution.ID, SessionReference: execution.SessionID.String(),
		Context: modelContext.Clone(),
		Model:   execution.Input.Model, MaxOutputTokens: executionModel.MaxOutputTokens, Tools: toolDefinitions(permissions, catalog),
		Execution: execution.Input.Runtime, TurnNumber: turn,
	}
}

func initialContext(
	ctx context.Context,
	execution executionmodel.AgentExecution,
	transcript runtimecontract.TranscriptStore,
) (appcontext.ExecutionContext, error) {
	modelContext := execution.Input.Context.Clone()
	messages, err := transcript.ListExecutionMessages(ctx, execution.ID)
	if err != nil {
		return appcontext.ExecutionContext{}, err
	}
	for _, message := range messages {
		if message.MessageID == "input:"+execution.RequestID.String() {
			continue
		}
		blocks := make([]appcontext.ContextBlock, len(message.Blocks))
		for index, block := range message.Blocks {
			blocks[index] = block.Clone()
		}
		if entry, ok := appcontext.EntryFromTranscriptMessage(appcontext.TranscriptMessage{
			Sequence: message.Sequence, ExecutionID: message.ExecutionID.String(),
			MessageID: message.MessageID, Digest: message.Digest,
			Role: message.Role, Content: message.Content, Blocks: blocks,
		}); ok {
			modelContext.Entries = append(modelContext.Entries, entry)
		}
	}
	if len(modelContext.Entries) == 0 {
		return appcontext.ExecutionContext{}, fail(contracts.ExecutionFailureStorage, ErrorStorage, "execution context has no entries")
	}
	return modelContext, nil
}

func appendProviderOutput(
	modelContext appcontext.ExecutionContext,
	blocks []appcontext.ContextBlock,
) appcontext.ExecutionContext {
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
	modelContext appcontext.ExecutionContext,
	call ToolCall,
	block appcontext.ContextBlock,
) appcontext.ExecutionContext {
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
	transcript runtimecontract.TranscriptStore,
	execution executionmodel.AgentExecution,
	turn int,
	blocks []appcontext.ContextBlock,
) error {
	return transcript.AppendStructuredMessage(
		ctx, execution.ID, fmt.Sprintf("assistant:%d", turn), "assistant", execution.RequestID, blocks,
	)
}

func appendToolResult(
	ctx context.Context,
	transcript runtimecontract.TranscriptStore,
	execution executionmodel.AgentExecution,
	turn int,
	callIndex int,
	call ToolCall,
	block appcontext.ContextBlock,
) error {
	return transcript.AppendStructuredMessage(
		ctx, execution.ID, fmt.Sprintf("tool-result:%d:%d:%s", turn, callIndex, call.ID),
		"tool", execution.RequestID, []appcontext.ContextBlock{block},
	)
}

func (e *ExecutionEngine) emit(event agent.AgentEvent) {
	if e.eventObserver != nil {
		e.eventObserver(event)
	}
}

func (e *ExecutionEngine) emitToolResult(
	execution executionmodel.AgentExecution,
	turn int,
	call ToolCall,
	block appcontext.ContextBlock,
) {
	e.emit(agent.AgentEvent{
		Kind:        agent.AgentEventToolResult,
		AgentID:     execution.AgentID,
		ExecutionID: execution.ID,
		Turn:        turn,
		CallID:      call.ID,
		Name:        string(call.Name),
		Result:      block.Text,
		IsError:     block.IsError,
	})
}

func (e *ExecutionEngine) emitError(execution executionmodel.AgentExecution, turn int, err error) {
	if err == nil {
		return
	}
	message := "execution failed"
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) && runtimeErr.Message != "" {
		message = runtimeErr.Message
	}
	e.emit(agent.AgentEvent{
		Kind:        agent.AgentEventError,
		AgentID:     execution.AgentID,
		ExecutionID: execution.ID,
		Turn:        turn,
		Error:       message,
	})
}

// executionBudget 执行单次 execution 的资源限制。
// 输出字节数跨 model turn 和工具结果累计，工具计数仅在调用成功后递增。
type executionBudget struct {
	limits       contracts.ResourceLimits
	maxTurns     int
	maxToolCalls int
	outputBytes  int64
	toolCalls    int
}

func newExecutionBudget(limits contracts.ResourceLimits) *executionBudget {
	budget := &executionBudget{limits: limits, maxTurns: limits.MaxTurns, maxToolCalls: limits.MaxToolCalls}
	if budget.maxTurns <= 0 {
		budget.maxTurns = defaultMaxTurns
	}
	if budget.maxToolCalls <= 0 {
		budget.maxToolCalls = defaultMaxToolCalls
	}
	return budget
}

func (b *executionBudget) reserveInput(request ModelRequest) error {
	if b.limits.MaxInputBytes > 0 && turnInputBytes(request) > b.limits.MaxInputBytes {
		return fail(contracts.ExecutionFailureResourceLimit, ErrorResourceLimit, "execution input limit exceeded")
	}
	return nil
}

func (b *executionBudget) addOutput(bytes int) error {
	b.outputBytes += int64(bytes)
	if b.limits.MaxOutputBytes > 0 && b.outputBytes > b.limits.MaxOutputBytes {
		return fail(contracts.ExecutionFailureResourceLimit, ErrorResourceLimit, "execution output limit exceeded")
	}
	return nil
}

func (b *executionBudget) reserveToolCalls(count int) error {
	if b.toolCalls+count > b.maxToolCalls {
		return fail(contracts.ExecutionFailureResourceLimit, ErrorResourceLimit, "execution tool call limit exceeded")
	}
	return nil
}

// executionFailure 携带引擎错误对应的 durable settlement 结果。
// settle 在统一的 run 边界把它转换为返回值。
type executionFailure struct {
	outcome executionmodel.ExecutionOutcome
	code    contracts.ExecutionFailureCode
	cause   error
}

func (f *executionFailure) Error() string {
	if f.cause == nil {
		return string(f.code)
	}
	return f.cause.Error()
}

func (f *executionFailure) Unwrap() error { return f.cause }

func fail(code contracts.ExecutionFailureCode, runtimeCode ErrorCode, message string) error {
	return &executionFailure{
		outcome: executionmodel.ExecutionFailed,
		code:    code,
		cause:   &RuntimeError{Code: runtimeCode, Message: message},
	}
}

func failCause(code contracts.ExecutionFailureCode, cause error) error {
	return &executionFailure{outcome: executionmodel.ExecutionFailed, code: code, cause: cause}
}

func settle(err error) (executionmodel.ExecutionOutcome, contracts.ExecutionFailureCode, error) {
	var failure *executionFailure
	if errors.As(err, &failure) {
		return failure.outcome, failure.code, failure.cause
	}
	return failedExecution(err)
}

func toolDefinitions(permissions contracts.ExecutionPermissions, catalog ToolCatalog) []ToolDefinition {
	if catalog == nil {
		return nil
	}
	definitions := make([]ToolDefinition, 0, len(permissions.AllowedTools))
	for _, definition := range catalog.List() {
		if permissions.AllowsTool(definition.Name) {
			definitions = append(definitions, definition.Snapshot())
		}
	}
	return definitions
}

func collectStream(ctx context.Context, stream <-chan ModelStreamEvent, onEvent func(ModelStreamEvent)) (string, []ToolCall, error) {
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
			case StreamTextDelta:
				text.WriteString(event.Text)
				if onEvent != nil && event.Text != "" {
					onEvent(event)
				}
			case StreamThinkingDelta:
				if onEvent != nil && event.Text != "" {
					onEvent(event)
				}
			case StreamToolCall:
				calls = append(calls, event.ToolCall)
				if onEvent != nil {
					onEvent(event)
				}
			case StreamComplete:
				return text.String(), calls, nil
			case StreamError:
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

func turnInputBytes(request ModelRequest) int64 {
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

func failedExecution(err error) (executionmodel.ExecutionOutcome, contracts.ExecutionFailureCode, error) {
	if err == nil {
		err = errors.New("execution engine failed")
	}
	return executionmodel.ExecutionFailed, contracts.ExecutionFailureRuntimeFailed, fmt.Errorf("execution engine: %w", err)
}
