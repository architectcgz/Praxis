package loop

import (
	"praxis/internal/contracts"
	sessionmodel "praxis/internal/core/session"
	turnmodel "praxis/internal/core/turn"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	runtimecontract "praxis/internal/agent_runtime"
	appcontext "praxis/internal/core/context"
)

const (
	defaultMaxSteps     = 32
	defaultMaxToolCalls = 128
)

// TurnModelBuilder 按已冻结的 turn 模型快照构建一次运行时模型。
type TurnModelBuilder interface {
	BuildTurnModel(contracts.ModelSnapshot) (TurnModel, error)
}

type TurnEngineConfig struct {
	ModelBuilder  TurnModelBuilder
	Tools         ToolCatalog
	ToolCalls     ToolCallHandler
	EventObserver runtimecontract.AgentEventObserver
	Logf          func(string, ...any)
}

// TurnEngine 运行一个已持久化的 Turn，直到得到结算结果。
// 它只负责 model/tool loop；执行状态仍由 Agent runtime 回调 settlement application service 更新。
type TurnEngine struct {
	modelBuilder  TurnModelBuilder
	tools         ToolCatalog
	toolCalls     ToolCallHandler
	eventObserver runtimecontract.AgentEventObserver
	logf          func(string, ...any)
}

func NewTurnEngine(config TurnEngineConfig) (*TurnEngine, error) {
	if config.ModelBuilder == nil {
		return nil, errors.New("turn model builder is required")
	}
	logf := config.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	return &TurnEngine{
		modelBuilder:  config.ModelBuilder,
		tools:         config.Tools,
		toolCalls:     config.ToolCalls,
		eventObserver: config.EventObserver,
		logf:          logf,
	}, nil
}

// RunWithSession 基于 Agent 自有消息流执行一次 turn，
// 并返回业务状态结算所需的结果。
func (e *TurnEngine) RunWithSession(
	ctx context.Context,
	turn turnmodel.Turn,
	messages runtimecontract.TurnMessageStore,
) (turnmodel.TurnOutcome, contracts.TurnFailureCode, error) {
	if messages == nil {
		return failedTurn(errors.New("turn message store is required"))
	}
	return e.run(ctx, turn, messages)
}

// run 是 turn step loop。每轮只发起一次模型请求；没有工具调用时结束，
// 否则必须先持久化每个工具调用结果，再构造下一次模型请求。
func (e *TurnEngine) run(
	ctx context.Context,
	turn turnmodel.Turn,
	messages runtimecontract.TurnMessageStore,
) (turnmodel.TurnOutcome, contracts.TurnFailureCode, error) {
	if ctx == nil {
		return failedTurn(errors.New("turn engine context is required"))
	}
	permissions := turn.Input.Security.Permissions
	modelContext, err := initialContext(ctx, turn, messages)
	if err != nil {
		e.emitError(turn, 0, err)
		return settle(err)
	}
	turnModel, err := e.buildModel(turn.Input.Model)
	if err != nil {
		e.emitError(turn, 0, err)
		return settle(err)
	}

	budget := newTurnBudget(permissions.ResourceLimits)
	for step := 1; step <= budget.maxSteps; step++ {
		e.emit(runtimecontract.AgentEvent{
			Kind:    runtimecontract.AgentEventStepStarted,
			AgentID: turn.AgentID,
			TurnID:  turn.ID,
			Step:    step,
		})
		assistant, calls, err := e.runStep(ctx, turn, turnModel, modelContext, budget, step)
		if err != nil {
			e.emitError(turn, step, err)
			return settle(err)
		}
		for _, call := range calls {
			if call.ID == "" || !call.Name.Valid() || len(call.Arguments) > 0 && !json.Valid(call.Arguments) {
				err := fail(contracts.TurnFailureContract, ErrorContract, "provider emitted an invalid tool call")
				e.emitError(turn, step, err)
				return settle(err)
			}
		}
		if len(assistant) > 0 {
			if err := appendAssistantMessage(ctx, messages, turn, step, assistant); err != nil {
				e.emitError(turn, step, err)
				return failedTurn(err)
			}
		}
		for _, call := range calls {
			e.emit(runtimecontract.AgentEvent{
				Kind:    runtimecontract.AgentEventToolCall,
				AgentID: turn.AgentID,
				TurnID:  turn.ID,
				Step:    step,
				CallID:  call.ID,
				Name:    string(call.Name),
				Input:   append([]byte(nil), call.Arguments...),
			})
		}
		e.emit(runtimecontract.AgentEvent{
			Kind:    runtimecontract.AgentEventStepCompleted,
			AgentID: turn.AgentID,
			TurnID:  turn.ID,
			Step:    step,
		})
		modelContext = appendProviderOutput(modelContext, assistant)
		if len(calls) == 0 {
			return turnmodel.TurnCompleted, "", nil
		}
		modelContext, err = e.runToolCalls(ctx, messages, turn, permissions, modelContext, calls, budget, step)
		if err != nil {
			e.emitError(turn, step, err)
			return settle(err)
		}
	}
	err = fail(contracts.TurnFailureResourceLimit, ErrorResourceLimit, "turn step limit exceeded")
	e.emitError(turn, budget.maxSteps, err)
	return settle(err)
}

func (e *TurnEngine) buildModel(snapshot contracts.ModelSnapshot) (TurnModel, error) {
	built, err := e.modelBuilder.BuildTurnModel(snapshot)
	if err != nil {
		return TurnModel{}, failCause(contracts.TurnFailureProvider, err)
	}
	if built.Stream == nil {
		return TurnModel{}, fail(contracts.TurnFailureProviderUnavailable, ErrorProvider, "turn model stream is unavailable")
	}
	return built, nil
}

// runStep 发起一次模型请求，按事件顺序返回 assistant blocks 和待派发工具调用。
func (e *TurnEngine) runStep(
	ctx context.Context,
	turn turnmodel.Turn,
	turnModel TurnModel,
	modelContext appcontext.ModelContext,
	budget *turnBudget,
	step int,
) ([]appcontext.ContextBlock, []ToolCall, error) {
	permissions := turn.Input.Security.Permissions
	request := buildModelRequest(turn, permissions, modelContext, turnModel, e.tools, step)
	if err := budget.reserveInput(request); err != nil {
		return nil, nil, err
	}
	e.logf("provider request prepared id=%s model=%s contextEntries=%d step=%d", turn.ID, turn.Input.Model.ModelID, len(modelContext.Entries), step)
	e.emit(runtimecontract.AgentEvent{
		Kind:    runtimecontract.AgentEventProviderWaiting,
		AgentID: turn.AgentID,
		TurnID:  turn.ID,
		Step:    step,
	})
	stream, err := turnModel.Stream.Stream(ctx, request)
	if err != nil {
		// 窗口判定在 provider adapter 里完成（只有它知道本协议序列化出的请求体）。
		var windowErr *ContextWindowExceededError
		if errors.As(err, &windowErr) {
			return nil, nil, fail(contracts.TurnFailureResourceLimit, ErrorResourceLimit, "model context window exceeded")
		}
		return nil, nil, failCause(contracts.TurnFailureProvider, err)
	}
	var blocks []appcontext.ContextBlock
	text, calls, err := collectStream(ctx, stream, func(event ModelStreamEvent) {
		if event.Kind == StreamUsage {
			e.emit(runtimecontract.AgentEvent{
				Kind:      runtimecontract.AgentEventModelUsage,
				SessionID: turn.SessionID,
				AgentID:   turn.AgentID,
				TurnID:    turn.ID,
				Step:      step,
				Usage:     event.Usage,
			})
			return
		}
		if event.Kind == StreamToolCall {
			call := event.ToolCall.Snapshot()
			blocks = append(blocks, appcontext.ContextBlock{
				Kind: appcontext.ContextBlockToolCall, CallID: call.ID, Name: string(call.Name), Input: append([]byte(nil), call.Arguments...),
			})
			return
		}
		kind := runtimecontract.AgentEventTextDelta
		blockKind := appcontext.ContextBlockText
		if event.Kind == StreamThinkingDelta {
			kind = runtimecontract.AgentEventThinkingDelta
			blockKind = appcontext.ContextBlockThinking
		}
		if len(blocks) > 0 && blocks[len(blocks)-1].Kind == blockKind {
			blocks[len(blocks)-1].Text += event.Text
		} else {
			blocks = append(blocks, appcontext.ContextBlock{Kind: blockKind, Text: event.Text})
		}
		e.emit(runtimecontract.AgentEvent{
			Kind:    kind,
			AgentID: turn.AgentID,
			TurnID:  turn.ID,
			Step:    step,
			Text:    event.Text,
		})
	})
	if err != nil {
		return nil, nil, failCause(contracts.TurnFailureProvider, err)
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

// runToolCalls 派发当前 step 的全部工具调用。
// 下一次模型请求读取结果前必须写入 durable tool-result receipt，
// 避免进程崩溃后留下无法对账的副作用。
func (e *TurnEngine) runToolCalls(
	ctx context.Context,
	messages runtimecontract.TurnMessageStore,
	turn turnmodel.Turn,
	permissions contracts.TurnPermissions,
	modelContext appcontext.ModelContext,
	calls []ToolCall,
	budget *turnBudget,
	step int,
) (appcontext.ModelContext, error) {
	if e.toolCalls == nil {
		return modelContext, fail(contracts.TurnFailureTool, ErrorTool, "tool capability is unavailable")
	}
	if err := budget.reserveToolCalls(len(calls)); err != nil {
		return modelContext, err
	}
	for callIndex, rawCall := range calls {
		call := rawCall
		result, err := e.toolCalls.Invoke(ctx, call, ToolInvocationMetadata{
			TurnID:              turn.ID,
			SessionID:           turn.SessionID,
			AgentID:             turn.AgentID,
			WorkspacePath:       turn.Input.WorkspacePath,
			SecurityFingerprint: turn.Input.Security.Fingerprint,
		})
		if err != nil {
			// 在结算失败前写入受限回执，确保人工检查时能看到已尝试的副作用。
			block := appcontext.ContextBlock{
				Kind: appcontext.ContextBlockToolResult, CallID: call.ID, Name: string(call.Name),
				Text: "tool execution failed", IsError: true,
			}
			if receiptErr := appendToolResult(ctx, messages, turn, step, callIndex, call, block); receiptErr != nil {
				return modelContext, failCause(contracts.TurnFailureStorage,
					&RuntimeError{Code: ErrorStorage, Message: "tool result receipt failed", Cause: receiptErr})
			}
			e.emitToolResult(turn, step, call, block)
			return modelContext, failCause(contracts.TurnFailureTool,
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
		if err := appendToolResult(ctx, messages, turn, step, callIndex, call, block); err != nil {
			return modelContext, err
		}
		e.emitToolResult(turn, step, call, block)
		modelContext = appendToolContextResult(modelContext, call, block)
	}
	return modelContext, nil
}

func buildModelRequest(
	turn turnmodel.Turn,
	permissions contracts.TurnPermissions,
	modelContext appcontext.ModelContext,
	turnModel TurnModel,
	catalog ToolCatalog,
	step int,
) ModelRequest {
	return ModelRequest{
		TurnID: turn.ID, SessionReference: turn.SessionID.String(),
		Context: modelContext.Clone(),
		Model:   turn.Input.Model, MaxOutputTokens: turnModel.MaxOutputTokens, Tools: toolDefinitions(permissions, catalog),
		Step: step,
	}
}

func initialContext(
	ctx context.Context,
	turn turnmodel.Turn,
	messages runtimecontract.TurnMessageStore,
) (appcontext.ModelContext, error) {
	modelContext := turn.Input.Context.Clone()
	turnMessages, err := messages.ListByTurn(ctx, turn.ID)
	if err != nil {
		return appcontext.ModelContext{}, err
	}
	for _, value := range turnMessages {
		if value.ID == "input:"+turn.RequestID.String() {
			continue
		}
		if entry, ok := appcontext.EntryFromMessageData(value); ok {
			modelContext.Entries = append(modelContext.Entries, entry)
		}
	}
	if len(modelContext.Entries) == 0 {
		return appcontext.ModelContext{}, fail(contracts.TurnFailureStorage, ErrorStorage, "turn context has no entries")
	}
	return modelContext, nil
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
	messages runtimecontract.TurnMessageStore,
	turn turnmodel.Turn,
	step int,
	blocks []appcontext.ContextBlock,
) error {
	return appendTurnMessage(ctx, messages, turn, fmt.Sprintf("assistant:%s:%d", turn.ID, step), sessionmodel.RoleAssistant, sessionmodel.AuthorAgent, string(turn.AgentID), blocks)
}

func appendToolResult(
	ctx context.Context,
	messages runtimecontract.TurnMessageStore,
	turn turnmodel.Turn,
	step int,
	callIndex int,
	call ToolCall,
	block appcontext.ContextBlock,
) error {
	return appendTurnMessage(ctx, messages, turn,
		fmt.Sprintf("tool-result:%s:%d:%d:%s", turn.ID, step, callIndex, call.ID),
		sessionmodel.RoleTool, sessionmodel.AuthorTool, string(call.Name), []appcontext.ContextBlock{block})
}

func appendTurnMessage(
	ctx context.Context,
	store runtimecontract.TurnMessageStore,
	turn turnmodel.Turn,
	id string,
	role sessionmodel.Role,
	authorKind sessionmodel.AuthorKind,
	authorID string,
	blocks []appcontext.ContextBlock,
) error {
	value := sessionmodel.MessageData{
		ID: id, RequestID: turn.RequestID.String(), TurnID: turn.ID.String(),
		Role: role, AuthorKind: authorKind, AuthorID: authorID,
		Blocks: make([]sessionmodel.Block, len(blocks)),
	}
	for index, block := range blocks {
		value.Blocks[index] = sessionmodel.Block{
			Kind: sessionmodel.BlockKind(block.Kind), Text: block.Text, CallID: block.CallID,
			Name: block.Name, Input: append([]byte(nil), block.Input...), IsError: block.IsError,
		}
	}
	_, err := store.Append(ctx, value)
	return err
}

func (e *TurnEngine) emit(event runtimecontract.AgentEvent) {
	if e.eventObserver != nil {
		e.eventObserver(event)
	}
}

func (e *TurnEngine) emitToolResult(
	turn turnmodel.Turn,
	step int,
	call ToolCall,
	block appcontext.ContextBlock,
) {
	e.emit(runtimecontract.AgentEvent{
		Kind:    runtimecontract.AgentEventToolResult,
		AgentID: turn.AgentID,
		TurnID:  turn.ID,
		Step:    step,
		CallID:  call.ID,
		Name:    string(call.Name),
		Result:  block.Text,
		IsError: block.IsError,
	})
}

func (e *TurnEngine) emitError(turn turnmodel.Turn, step int, err error) {
	if err == nil {
		return
	}
	message := "turn failed"
	var runtimeErr *RuntimeError
	if errors.As(err, &runtimeErr) && runtimeErr.Message != "" {
		message = runtimeErr.Message
	} else {
		var failure *turnFailure
		if errors.As(err, &failure) {
			if detail := contracts.TurnFailureMessage(failure.code, failure.cause); detail != "" {
				message = detail
			}
		}
	}
	e.emit(runtimecontract.AgentEvent{
		Kind:    runtimecontract.AgentEventError,
		AgentID: turn.AgentID,
		TurnID:  turn.ID,
		Step:    step,
		Error:   message,
	})
}

// turnBudget 执行单次 turn 的资源限制。
// 输出字节数跨 model step 和工具结果累计，工具计数仅在调用成功后递增。
type turnBudget struct {
	limits       contracts.ResourceLimits
	maxSteps     int
	maxToolCalls int
	outputBytes  int64
	toolCalls    int
}

func newTurnBudget(limits contracts.ResourceLimits) *turnBudget {
	budget := &turnBudget{limits: limits, maxSteps: limits.MaxSteps, maxToolCalls: limits.MaxToolCalls}
	if budget.maxSteps <= 0 {
		budget.maxSteps = defaultMaxSteps
	}
	if budget.maxToolCalls <= 0 {
		budget.maxToolCalls = defaultMaxToolCalls
	}
	return budget
}

func (b *turnBudget) reserveInput(request ModelRequest) error {
	if b.limits.MaxInputBytes > 0 && stepInputBytes(request) > b.limits.MaxInputBytes {
		return fail(contracts.TurnFailureResourceLimit, ErrorResourceLimit, "turn input limit exceeded")
	}
	return nil
}

func (b *turnBudget) addOutput(bytes int) error {
	b.outputBytes += int64(bytes)
	if b.limits.MaxOutputBytes > 0 && b.outputBytes > b.limits.MaxOutputBytes {
		return fail(contracts.TurnFailureResourceLimit, ErrorResourceLimit, "turn output limit exceeded")
	}
	return nil
}

func (b *turnBudget) reserveToolCalls(count int) error {
	if b.toolCalls+count > b.maxToolCalls {
		return fail(contracts.TurnFailureResourceLimit, ErrorResourceLimit, "turn tool call limit exceeded")
	}
	return nil
}

// turnFailure 携带引擎错误对应的 durable settlement 结果。
// settle 在统一的 run 边界把它转换为返回值。
type turnFailure struct {
	outcome turnmodel.TurnOutcome
	code    contracts.TurnFailureCode
	cause   error
}

func (f *turnFailure) Error() string {
	if f.cause == nil {
		return string(f.code)
	}
	return f.cause.Error()
}

func (f *turnFailure) Unwrap() error { return f.cause }

func fail(code contracts.TurnFailureCode, runtimeCode ErrorCode, message string) error {
	return &turnFailure{
		outcome: turnmodel.TurnFailed,
		code:    code,
		cause:   &RuntimeError{Code: runtimeCode, Message: message},
	}
}

func failCause(code contracts.TurnFailureCode, cause error) error {
	return &turnFailure{outcome: turnmodel.TurnFailed, code: code, cause: cause}
}

func settle(err error) (turnmodel.TurnOutcome, contracts.TurnFailureCode, error) {
	var failure *turnFailure
	if errors.As(err, &failure) {
		return failure.outcome, failure.code, failure.cause
	}
	return failedTurn(err)
}

func toolDefinitions(permissions contracts.TurnPermissions, catalog ToolCatalog) []ToolDefinition {
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
				event.ToolCall = event.ToolCall.Snapshot()
				calls = append(calls, event.ToolCall)
				if onEvent != nil {
					onEvent(event)
				}
			case StreamUsage:
				if event.Usage != nil && event.Usage.Valid() && onEvent != nil {
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

func stepInputBytes(request ModelRequest) int64 {
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

func failedTurn(err error) (turnmodel.TurnOutcome, contracts.TurnFailureCode, error) {
	if err == nil {
		err = errors.New("turn engine failed")
	}
	return turnmodel.TurnFailed, contracts.TurnFailureRuntimeFailed, fmt.Errorf("turn engine: %w", err)
}
