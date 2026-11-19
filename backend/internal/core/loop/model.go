package loop

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/entry"
	coreevent "praxis/internal/core/event"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	modelusage "praxis/internal/core/model/usage"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
	"slices"
	"strings"
	"time"
)

// Response 保存一次模型响应；内容块和工具调用都从同一份有序事件转换。
type Response struct {
	RequestID  identity.ModelRequestID
	events     []providerapi.Event
	textBytes  int
	StopReason providerapi.StopReason
	Usage      *modelusage.Usage
	Timing     *timing.Record
}

func (r Response) Blocks() []sessionentry.ContentBlock {
	return entryBlocksFromEvents(r.events)
}

// requestModel 只发起请求和消费模型事件，不读取 Run、不记录进度或触发 Hook。
func requestModel(
	ctx context.Context,
	modelProvider providerapi.Provider,
	request providerapi.Request,
	onEvent func(providerapi.Event),
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
	response, err := collectModelResponse(streamCtx, input, onEvent)
	if err == nil {
		err = response.bindReasoningState(request.Model)
	}
	return response, err
}

// saveModelRequest 只在提交成功后发布请求活动，调用者必须处于所属 Run 的串行执行点。
func (r *Runner) saveModelRequest(ctx context.Context, params RunParams, appendEntry AppendEntry, request sessionentry.ModelRequest) error {
	suffix := "finished"
	if request.Status == sessionentry.RequestRunning {
		suffix = "started"
	}
	stored, _, err := appendEntry(ctx, sessionentry.Entry{
		ID:      "request:" + request.ID.String() + ":" + suffix,
		RunID:   request.RunID.String(),
		TurnID:  request.TurnID.String(),
		Payload: request,
	})
	if err != nil {
		return err
	}
	r.emit(params, request.TurnID, modelRequestEvent(request, stored.CreatedAt, stored.HistoryEnd))
	return nil
}

func modelRequestEvent(request sessionentry.ModelRequest, createdAt time.Time, historyEnd int64) coreevent.Payload {
	info := coreevent.ModelRequestInfo{
		RequestID:  request.ID,
		Kind:       request.Kind,
		RetryCount: request.RetryCount,
		CreatedAt:  createdAt,
		HistoryEnd: historyEnd,
		Timing:     request.Timing,
	}
	switch request.Status {
	case sessionentry.RequestRunning:
		return coreevent.ModelRequestStarted{Info: info}
	case sessionentry.RequestCompleted:
		return coreevent.ModelRequestCompleted{Info: info, StopReason: request.StopReason, Usage: request.Usage}
	}
	failure := coreevent.ModelRequestFailed{
		Info:       info,
		StopReason: request.StopReason,
		Error:      request.Error,
		Partial:    request.PartialText(),
		Usage:      request.Usage,
	}
	if request.Status == sessionentry.RequestInterrupted {
		return coreevent.ModelRequestInterrupted(failure)
	}
	return failure
}

// callModel 在模型请求前后记录执行事实和计时。
func (r *Runner) callModel(
	ctx context.Context,
	params RunParams,
	deps RunDependencies,
	request providerapi.Request,
	requestKind sessionentry.ModelRequestKind,
	retryCount int,
	onEvent func(providerapi.Event),
) (response Response, err error) {
	boundary := hook.ModelRequest{
		Identity: runIdentity(params, request.TurnID),
		Kind:     requestKind,
		Request:  request,
	}
	boundary.OperationID = hook.NewOperationID()
	boundary.Identity.RequestID = request.ID
	if hookErr := r.HookDispatcher.BeforeModelRequest(ctx, boundary); hookErr != nil {
		return response, NewFailure(FailureInternal, hookErr)
	}
	observationContext := request.Context
	if requestKind == sessionentry.ModelRequestIteration {
		request, err = remindContextBudget(request, deps.Budget)
		if err != nil {
			return response, NewModelFailure(err)
		}
	}
	if requestKind != sessionentry.ModelRequestIteration {
		tokens, _, budgetErr := requestTokens(request, nil)
		if budgetErr != nil {
			return response, NewFailure(FailureContract, budgetErr)
		}
		if capacityErr := appcontext.CheckCapacity(tokens, request.MaxOutputTokens, request.Model.ContextWindow); capacityErr != nil {
			return response, NewModelFailure(capacityErr)
		}
	}
	if err := ctx.Err(); err != nil {
		return response, err
	}
	span := timing.NewSpan()

	started := sessionentry.ModelRequest{
		ID:         request.ID,
		RunID:      params.ID,
		TurnID:     request.TurnID,
		Kind:       requestKind,
		Status:     sessionentry.RequestRunning,
		RetryCount: retryCount,
		Timing:     span.Snapshot(),
	}
	if err := r.saveModelRequest(ctx, params, deps.AppendEntry, started); err != nil {
		return response, err
	}

	boundary.Request = request
	ctx, scope := r.HookDispatcher.Start(ctx, hook.Operation{
		Identity:    boundary.Identity,
		Kind:        hook.OperationProvider,
		Name:        request.Model.ProviderID + "/" + request.Model.ModelID,
		ReferenceID: request.ID.String(),
	})
	defer func() {
		// 取消或断流也保存已知请求事实，明细与累计快照使用同一次日志提交。
		finished := sessionentry.ModelRequest{
			ID:          request.ID,
			RunID:       params.ID,
			TurnID:      request.TurnID,
			Kind:        requestKind,
			Status:      sessionentry.RequestStatus(ctx, err),
			StopReason:  string(response.StopReason),
			OutputBytes: response.textBytes,
			Usage:       response.Usage,
			RetryCount:  retryCount,
			Timing:      response.Timing,
		}
		if err != nil {
			finished.Error = err.Error()
			finished.Partial = response.Blocks()
		}
		if saveErr := r.saveModelRequest(context.WithoutCancel(ctx), params, deps.AppendEntry, finished); saveErr != nil {
			err = NewFailure(FailureStorage, errors.Join(err, saveErr))
		}

		if hookErr := r.HookDispatcher.AfterModelResponse(context.WithoutCancel(ctx), hook.ModelResponse{
			Request:     boundary,
			OutputBytes: response.textBytes,
			StopReason:  response.StopReason,
			Err:         err,
		}); hookErr != nil {
			r.Logf("after model response hook failed: %v", hookErr)
		}
		scope.End(hook.Result{Err: err, StopReason: response.StopReason})
	}()
	span.Start()
	firstResponse := false
	response, err = requestModel(ctx, r.Provider, request, func(event providerapi.Event) {
		if !firstResponse && (event.Kind == providerapi.StreamTextDelta || event.Kind == providerapi.StreamThinkingDelta || event.Kind == providerapi.StreamToolCall) {
			firstResponse = true
			span.FirstResponse()
			scope.OnSignal(hook.SignalFirstResponse)
		}
		if onEvent != nil {
			onEvent(event)
		}
	})
	response.Timing = span.End()
	response.RequestID = request.ID
	if requestKind == sessionentry.ModelRequestIteration && err == nil {
		_, key, shapeErr := requestTokens(request, deps.Budget)
		if shapeErr != nil {
			return response, NewFailure(FailureContract, shapeErr)
		}
		tokens := 0
		if response.Usage != nil && response.Usage.InputTokens != nil {
			tokens = int(*response.Usage.InputTokens)
		}
		if deps.Budget != nil {
			deps.Budget.Observe(observationContext, key, tokens)
		}
	}
	return response, err
}

// collectModelResponse 只消费模型事件流；预算、任务失败分类和实时事件映射由调用方负责。
// 相邻文本增量合并为内容事件，用量保留最后累计快照；失败时返回已收到的内容和统计。
func collectModelResponse(
	ctx context.Context,
	stream <-chan providerapi.Event,
	onEvent func(providerapi.Event),
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
		case providerapi.StreamTextDelta, providerapi.StreamThinkingDelta:
			if event.Kind == providerapi.StreamTextDelta {
				response.textBytes += len(event.Text)
			}
			last := len(response.events) - 1
			if last >= 0 && response.events[last].Kind == event.Kind && sameOutputIndex(response.events[last].OutputIndex, event.OutputIndex) {
				response.events[last].Text += event.Text
			} else {
				response.events = append(response.events, providerapi.Event{Kind: event.Kind, Text: event.Text, OutputIndex: event.OutputIndex})
			}
		case providerapi.StreamToolCall:
			response.events = append(response.events, providerapi.Event{Kind: providerapi.StreamToolCall, ToolCall: event.ToolCall, OutputIndex: event.OutputIndex})
		case providerapi.StreamReasoningState:
			response.events = append(response.events, providerapi.Event{
				Kind:           providerapi.StreamReasoningState,
				ReasoningState: event.ReasoningState,
				OutputIndex:    event.OutputIndex,
			})
			// 推理状态仅保存在内部，观察者不接收签名或加密载荷。
			continue
		case providerapi.StreamUsage:
			response.Usage = event.Usage
		case providerapi.StreamComplete:
			response.StopReason = event.StopReason
			indexed := len(response.events) > 0 && response.events[0].OutputIndex != nil
			for _, content := range response.events {
				if (content.OutputIndex != nil) != indexed || content.OutputIndex != nil && *content.OutputIndex < 0 {
					return response, errors.New("provider emitted inconsistent output ordering")
				}
			}
			// 输出块按协议顺序重放；工具事件可在流终态才发送，不改变它在 Entry 中的位置。
			slices.SortStableFunc(response.events, func(a, b providerapi.Event) int {
				if a.OutputIndex == nil || b.OutputIndex == nil {
					return 0
				}
				return cmp.Compare(*a.OutputIndex, *b.OutputIndex)
			})
			return response, response.stopError()
		case providerapi.StreamError:
			if event.Err != nil {
				return response, event.Err
			}
			return response, errors.New("provider stream failed")
		default:
			return response, errors.New("provider stream emitted an unknown event")
		}
		if onEvent != nil {
			onEvent(event)
		}
	}
}

// stopError 将当前 Loop 无法继续处理的结束状态转换为失败，摘要请求使用同一策略。
func (r Response) stopError() error {
	switch r.StopReason {
	case providerapi.StopTruncated:
		return errors.New("model output was truncated")
	case providerapi.StopFiltered:
		return errors.New("model output was filtered")
	case providerapi.StopRefused:
		return errors.New("model refused the request")
	case providerapi.StopPaused:
		return errors.New("model paused the turn; continuation is not supported")
	default:
		return nil
	}
}

func (r Response) ToolCalls() []ToolCall {
	calls := make([]ToolCall, 0)
	for _, event := range r.events {
		if event.Kind == providerapi.StreamToolCall {
			calls = append(calls, event.ToolCall)
		}
	}
	return calls
}

func sameOutputIndex(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// bindReasoningState 确认响应协议后绑定冻结的请求身份，不重复解析 Decoder 已处理的载荷。
func (r *Response) bindReasoningState(config modelapi.Config) error {
	for _, event := range r.events {
		if event.Kind != providerapi.StreamReasoningState {
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

// summarizeContext 在 Turn 之间生成归属于 Run 的摘要，不执行工具或发布回答文本。
func (r *Runner) summarizeContext(ctx context.Context, params RunParams, deps RunDependencies, input appcontext.ModelContext, limit int) (string, identity.ModelRequestID, identity.TurnID, error) {
	var turnID identity.TurnID
	requestID := identity.NewModelRequestID()
	request := providerapi.Request{
		ID:              requestID,
		TurnID:          turnID,
		RunID:           params.ID,
		PromptCacheKey:  params.SessionID.String(),
		Context:         input,
		Model:           params.Model,
		MaxOutputTokens: limit,
	}
	requestCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	response, err := r.Request(requestCtx, params, deps, request, sessionentry.ModelRequestSummary)
	if err != nil {
		return "", response.RequestID, turnID, err
	}
	if len(response.ToolCalls()) != 0 {
		return "", response.RequestID, turnID, errors.New("context summary returned a tool call")
	}
	var output strings.Builder
	for _, event := range response.events {
		if event.Kind == providerapi.StreamTextDelta {
			output.WriteString(event.Text)
		}
	}
	return strings.TrimSpace(output.String()), response.RequestID, turnID, nil
}
