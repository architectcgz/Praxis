// Package modelcall 提供统一的底层模型请求执行能力，供对话迭代、上下文摘要与风险审核共用。
package modelcall

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	appcontext "praxis/internal/context"
	"praxis/internal/engine/hook"
	"praxis/internal/entry"
	"praxis/internal/errorcode"
	"praxis/internal/event"
	"praxis/internal/identity"
	"praxis/internal/model"
	"praxis/internal/provider"
	"praxis/internal/timing"
	"praxis/internal/tools"
	"slices"
	"strings"
	"time"
)

type Dependencies struct {
	HookDispatcher *hook.Dispatcher
	Emitter        *event.Emitter
	Logf           func(string, ...any)
	MaxAttempts    int
	RequestTimeout time.Duration
	CommitTimeout  time.Duration
	BudgetPolicy   appcontext.BudgetPolicy
}

// Caller 封装模型请求的容量校验、Hook 观测、计时、事实持久化、流消费与重试策略。
type Caller struct {
	deps Dependencies
}

func NewCaller(deps Dependencies) *Caller {
	if deps.Logf == nil {
		deps.Logf = func(string, ...any) {}
	}
	return &Caller{deps: deps}
}

// Call 描述一次模型请求的身份、预算、回调与响应校验约束。
type Call struct {
	Provider        provider.Provider
	Request         provider.Request
	Kind            entry.ModelRequestKind
	SessionID       identity.SessionID
	AgentID         identity.AgentID
	SourceTaskID    identity.TaskID
	CallID          string
	Budget          *appcontext.Budget
	Prepare         func(provider.Request) (provider.Request, error)
	Validate        func(Response) error
	MaxTextBytes    int
	AllowTools      bool
	OnProviderEvent func(provider.Event)
	AppendEntry     entry.AppendEntry
}

// Response 保存一次模型响应；内容块和工具调用都从同一份有序事件转换。
type Response struct {
	RequestID  identity.ModelRequestID
	Attempts   int
	events     []provider.Event
	TextBytes  int
	StopReason provider.StopReason
	Usage      *model.Usage
	Timing     *timing.Timing
}

func (r Response) Blocks() []entry.ContentBlock {
	blocks := make([]entry.ContentBlock, 0, len(r.events))
	for _, event := range r.events {
		if event.Kind == provider.StreamReasoningState {
			blocks = append(blocks, entry.ReasoningStateBlock{
				State: event.ReasoningState,
			})
			continue
		}
		if event.Kind == provider.StreamToolCall {
			call := event.ToolCall
			blocks = append(blocks, entry.ToolCallBlock{
				CallID:    call.ID,
				Name:      call.Name,
				Arguments: call.Arguments,
			})
			continue
		}
		if (event.Kind != provider.StreamTextDelta && event.Kind != provider.StreamThinkingDelta) || strings.TrimSpace(event.Text) == "" {
			continue
		}
		if event.Kind == provider.StreamThinkingDelta {
			blocks = append(blocks, entry.ThinkingBlock{Text: event.Text})
		} else {
			blocks = append(blocks, entry.TextBlock{Text: event.Text})
		}
	}
	return blocks
}

func (r Response) Text() string {
	var text strings.Builder
	for _, event := range r.events {
		if event.Kind == provider.StreamTextDelta {
			text.WriteString(event.Text)
		}
	}
	return text.String()
}

func (r Response) ToolCalls() []provider.ToolCall {
	calls := make([]provider.ToolCall, 0)
	for _, event := range r.events {
		if event.Kind == provider.StreamToolCall {
			calls = append(calls, event.ToolCall)
		}
	}
	return calls
}

// Do 在同一个请求预算内重试供应商调用，不重放已经执行的工具或整个 Run。
func (c *Caller) Do(ctx context.Context, call Call) (Response, error) {
	requestCtx, cancel := context.WithTimeout(ctx, c.deps.RequestTimeout)
	defer cancel()
	var response Response
	var err error
	for retry := range c.deps.MaxAttempts {
		if requestCtx.Err() != nil {
			break
		}
		call.Request.ID = identity.NewModelRequestID()
		response, err = c.doAttempt(requestCtx, call, retry)
		response.Attempts = retry + 1
		if err == nil {
			return response, nil
		}
		label := "Model request"
		if call.Kind == entry.ModelRequestRiskReview {
			label = "Risk review"
		}
		c.emit(call, event.ErrorOccurred{
			Message: fmt.Sprintf("%s failed (attempt %d/%d): %v", label, response.Attempts, c.deps.MaxAttempts, err),
		})
		if errors.Is(err, errorcode.ErrStorage) {
			return response, err
		}
		requestErr := requestCtx.Err()
		// 只将裸上下文错误交回请求预算处理；即使显式错误包装了同一原因，也要保留。
		if !errors.Is(err, errorcode.ErrProvider) && err != requestErr {
			return response, err
		}
		if requestErr != nil {
			break
		}
	}
	if ctx.Err() != nil {
		return response, ctx.Err()
	}
	if requestCtx.Err() != nil {
		return response, errorcode.ErrProvider.Wrap(fmt.Errorf("model request exceeded %s: %w", c.deps.RequestTimeout, requestCtx.Err()))
	}
	return response, err
}

func (c *Caller) doAttempt(ctx context.Context, call Call, retryCount int) (response Response, err error) {
	request := call.Request
	boundary := hook.ModelRequest{
		Identity: hook.Identity{
			OperationID:  identity.NewOperationID(),
			SourceTaskID: call.SourceTaskID,
			SessionID:    call.SessionID,
			AgentID:      call.AgentID,
			RunID:        request.RunID,
			TurnID:       request.TurnID,
			RequestID:    request.ID,
		},
		Kind:    call.Kind,
		Request: request,
	}
	if hookErr := c.deps.HookDispatcher.BeforeModelRequest(ctx, boundary); hookErr != nil {
		return response, errorcode.ErrInternal.Wrap(hookErr)
	}
	observationContext := request.Context
	if call.Prepare != nil {
		request, err = call.Prepare(request)
		if err != nil {
			return response, WrapModelError(err)
		}
	} else {
		tokens, budgetErr := c.EstimateRequestTokens(request, nil)
		if budgetErr != nil {
			return response, errorcode.ErrContract.Wrap(budgetErr)
		}
		if capacityErr := c.deps.BudgetPolicy.CheckCapacity(tokens, request.MaxOutputTokens, request.Model.ContextWindow); capacityErr != nil {
			return response, capacityErr
		}
	}
	if err := ctx.Err(); err != nil {
		return response, err
	}
	span := timing.NewSpan()
	started := entry.ModelRequest{
		ID:         request.ID,
		RunID:      request.RunID,
		TurnID:     request.TurnID,
		CallID:     call.CallID,
		Kind:       call.Kind,
		Status:     entry.RequestRunning,
		RetryCount: retryCount,
		Timing:     span.Snapshot(),
	}
	if err := c.saveModelRequest(ctx, call, started); err != nil {
		return response, err
	}

	boundary.Request = request
	ctx, scope := c.deps.HookDispatcher.Start(ctx, hook.Operation{
		Identity:    boundary.Identity,
		Kind:        hook.OperationProvider,
		Name:        request.Model.ProviderID + "/" + request.Model.ModelID,
		ReferenceID: request.ID.String(),
	})
	defer func() {
		finished := entry.ModelRequest{
			ID:          request.ID,
			RunID:       request.RunID,
			TurnID:      request.TurnID,
			CallID:      call.CallID,
			Kind:        call.Kind,
			Status:      entry.RequestStatus(ctx, err),
			StopReason:  string(response.StopReason),
			OutputBytes: response.TextBytes,
			Usage:       response.Usage,
			RetryCount:  retryCount,
			Timing:      response.Timing,
		}
		if err != nil {
			finished.Error = err.Error()
			finished.Partial = response.Blocks()
		}
		commitCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.deps.CommitTimeout)
		defer cancel()
		if saveErr := c.saveModelRequest(commitCtx, call, finished); saveErr != nil {
			err = errorcode.ErrStorage.Wrap(errors.Join(err, saveErr))
		}
		if hookErr := c.deps.HookDispatcher.AfterModelResponse(commitCtx, hook.ModelResponse{
			Request:     boundary,
			OutputBytes: response.TextBytes,
			StopReason:  response.StopReason,
			Err:         err,
		}); hookErr != nil {
			c.deps.Logf("after model response hook failed: %v", hookErr)
		}
		scope.End(hook.Result{Err: err, StopReason: response.StopReason})
	}()

	span.Start()
	firstResponse := false
	response, err = streamModel(ctx, call.Provider, request, call.AllowTools, call.MaxTextBytes, func(event provider.Event) {
		if !firstResponse && (event.Kind == provider.StreamTextDelta || event.Kind == provider.StreamThinkingDelta || event.Kind == provider.StreamToolCall) {
			firstResponse = true
			span.FirstResponse()
			scope.OnSignal(hook.SignalFirstResponse)
		}
		if call.OnProviderEvent != nil {
			call.OnProviderEvent(event)
		}
	})
	response.Timing = span.End()
	response.RequestID = request.ID
	if err == nil && call.Validate != nil {
		err = call.Validate(response)
	}
	if err != nil {
		err = WrapModelError(err)
		return response, err
	}
	if call.Budget != nil {
		key, shapeErr := requestShapeKey(request)
		if shapeErr != nil {
			return response, errorcode.ErrContract.Wrap(shapeErr)
		}
		tokens := 0
		if response.Usage != nil && response.Usage.InputTokens != nil {
			tokens = int(*response.Usage.InputTokens)
		}
		call.Budget.Observe(observationContext, key, tokens)
	}
	return response, nil
}

func (c *Caller) saveModelRequest(ctx context.Context, call Call, request entry.ModelRequest) error {
	stored, _, err := call.AppendEntry(ctx, entry.Entry{
		ID:      identity.NewEntryID(),
		RunID:   request.RunID,
		TurnID:  request.TurnID,
		Payload: request,
	})
	if err != nil {
		return err
	}
	c.emit(call, modelRequestEvent(request, stored.CreatedAt, stored.HistoryEnd))
	return nil
}

func (c *Caller) emit(call Call, payload event.Payload) {
	c.deps.Emitter.Emit(event.Event{
		Source: event.EventSource{
			SessionID: call.SessionID,
			AgentID:   call.AgentID,
			RunID:     call.Request.RunID,
			TurnID:    call.Request.TurnID,
		},
		Payload: payload,
	})
}

func modelRequestEvent(request entry.ModelRequest, createdAt time.Time, historyEnd int64) event.Payload {
	info := event.ModelRequestInfo{
		RequestID:  request.ID,
		Kind:       request.Kind,
		RetryCount: request.RetryCount,
		CreatedAt:  createdAt,
		HistoryEnd: historyEnd,
		Timing:     request.Timing,
	}
	switch request.Status {
	case entry.RequestRunning:
		return event.ModelRequestStarted{Info: info}
	case entry.RequestCompleted:
		return event.ModelRequestCompleted{
			Info:       info,
			StopReason: request.StopReason,
			Usage:      request.Usage,
		}
	}
	failure := event.ModelRequestFailed{
		Info:       info,
		StopReason: request.StopReason,
		Error:      request.Error,
		Partial:    request.PartialText(),
		Usage:      request.Usage,
	}
	if request.Status == entry.RequestInterrupted {
		return event.ModelRequestInterrupted(failure)
	}
	return failure
}

// WrapModelError 保留已有存储、契约、内部和容量错误，其余模型调用异常归类为供应商错误。
func WrapModelError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, errorcode.ErrStorage) || errors.Is(err, errorcode.ErrContract) || errors.Is(err, errorcode.ErrInternal) {
		return err
	}
	if _, ok := errors.AsType[*appcontext.ContextWindowExceededError](err); ok {
		return err
	}
	return errorcode.ErrProvider.Wrap(err)
}

// EstimateRequestTokens 估算完整模型请求（含工具定义、输出 Schema 与请求预留）的输入 token。
func (c *Caller) EstimateRequestTokens(request provider.Request, budget *appcontext.Budget) (int, error) {
	key, overhead, err := requestShape(request)
	if err != nil {
		return 0, err
	}
	reserved := overhead + c.deps.BudgetPolicy.RequestReserveTokens
	if budget != nil {
		return budget.Estimate(request.Context, key, reserved)
	}
	tokens, err := request.Context.EstimatedTokens()
	return tokens + reserved, err
}

// EstimateOverhead 估算工具定义与结构化输出 Schema 占用的请求开销。
func EstimateOverhead(definitions []tools.ToolDefinition, schema *provider.OutputSchema) (int, error) {
	data, err := json.Marshal(struct {
		Tools        []tools.ToolDefinition
		OutputSchema *provider.OutputSchema
	}{
		Tools:        definitions,
		OutputSchema: schema,
	})
	if err != nil {
		return 0, err
	}
	return appcontext.EstimateTokens(string(data)), nil
}

func requestShapeKey(request provider.Request) (string, error) {
	data, err := json.Marshal(struct {
		Model  any
		Tools  []tools.ToolDefinition
		Schema *provider.OutputSchema
	}{
		Model:  request.Model,
		Tools:  request.Tools,
		Schema: request.OutputSchema,
	})
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func requestShape(request provider.Request) (string, int, error) {
	key, err := requestShapeKey(request)
	if err != nil {
		return "", 0, err
	}
	overhead, err := EstimateOverhead(request.Tools, request.OutputSchema)
	return key, overhead, err
}

func streamModel(
	ctx context.Context,
	modelProvider provider.Provider,
	request provider.Request,
	allowTools bool,
	maxTextBytes int,
	onProviderEvent func(provider.Event),
) (Response, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	input, err := modelProvider.Stream(streamCtx, request)
	if err != nil {
		return Response{}, err
	}
	if input == nil {
		return Response{}, errors.New("provider returned a nil stream")
	}
	defer func() {
		cancel()
		for range input {
		}
	}()
	response, err := collectResponse(streamCtx, input, allowTools, maxTextBytes, onProviderEvent)
	if err == nil {
		err = response.bindReasoningState(request.Model)
	}
	return response, err
}

func collectResponse(
	ctx context.Context,
	stream <-chan provider.Event,
	allowTools bool,
	maxTextBytes int,
	onProviderEvent func(provider.Event),
) (Response, error) {
	var response Response
	for {
		event, ok := <-stream
		if !ok {
			if ctx.Err() != nil {
				return response, ctx.Err()
			}
			return response, fmt.Errorf("provider stream ended before a terminal event: %w", io.ErrUnexpectedEOF)
		}
		switch event.Kind {
		case provider.StreamTextDelta, provider.StreamThinkingDelta:
			if event.Kind == provider.StreamTextDelta {
				if maxTextBytes > 0 && response.TextBytes+len(event.Text) > maxTextBytes {
					return response, fmt.Errorf("model output exceeds %d bytes", maxTextBytes)
				}
				response.TextBytes += len(event.Text)
			}
			last := len(response.events) - 1
			if last >= 0 && response.events[last].Kind == event.Kind && sameOutputIndex(response.events[last].OutputIndex, event.OutputIndex) {
				response.events[last].Text += event.Text
			} else {
				response.events = append(response.events, provider.Event{
					Kind:        event.Kind,
					Text:        event.Text,
					OutputIndex: event.OutputIndex,
				})
			}
		case provider.StreamToolCall:
			if !allowTools {
				return response, errors.New("model request emitted an unsupported tool call")
			}
			response.events = append(response.events, provider.Event{
				Kind:        provider.StreamToolCall,
				ToolCall:    event.ToolCall,
				OutputIndex: event.OutputIndex,
			})
		case provider.StreamReasoningState:
			response.events = append(response.events, provider.Event{
				Kind:           provider.StreamReasoningState,
				ReasoningState: event.ReasoningState,
				OutputIndex:    event.OutputIndex,
			})
			continue
		case provider.StreamUsage:
			response.Usage = event.Usage
		case provider.StreamComplete:
			response.StopReason = event.StopReason
			indexed := len(response.events) > 0 && response.events[0].OutputIndex != nil
			for _, content := range response.events {
				if (content.OutputIndex != nil) != indexed || content.OutputIndex != nil && *content.OutputIndex < 0 {
					return response, errors.New("provider emitted inconsistent output ordering")
				}
			}
			slices.SortStableFunc(response.events, func(a, b provider.Event) int {
				if a.OutputIndex == nil || b.OutputIndex == nil {
					return 0
				}
				return cmp.Compare(*a.OutputIndex, *b.OutputIndex)
			})
			return response, response.stopError()
		case provider.StreamError:
			if event.Err != nil {
				return response, event.Err
			}
			return response, errors.New("provider stream failed")
		default:
			return response, errors.New("provider stream emitted an unknown event")
		}
		if onProviderEvent != nil {
			onProviderEvent(event)
		}
	}
}

func (r Response) stopError() error {
	switch r.StopReason {
	case provider.StopTruncated:
		return errors.New("model output was truncated")
	case provider.StopFiltered:
		return errors.New("model output was filtered")
	case provider.StopRefused:
		return errors.New("model refused the request")
	case provider.StopPaused:
		return errors.New("model paused the turn; continuation is not supported")
	default:
		return nil
	}
}

func sameOutputIndex(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

func (r *Response) bindReasoningState(config model.Config) error {
	for _, event := range r.events {
		if event.Kind != provider.StreamReasoningState {
			continue
		}
		state := event.ReasoningState
		if state.Protocol != config.APIFormat {
			return errors.New("reasoning state does not match the frozen request protocol")
		}
		state.ProviderID = config.ProviderID
		state.ModelID = config.ModelID
	}
	return nil
}
