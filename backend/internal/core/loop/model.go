package loop

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/agent/run/timing"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"slices"
)

// Response 保存一次模型响应；内容块和工具调用都从同一份有序事件转换。
type Response struct {
	events     []providerapi.Event
	textBytes  int
	StopReason providerapi.StopReason
	Usage      *providerapi.Usage
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
	response, err := collectModelResponse(streamCtx, input, onEvent)
	if err == nil {
		err = response.bindReasoningState(request.Model)
	}
	return response, err
}

// callModel 在固定位置执行请求通知、原生进度记录与模型计时。
func (r *Runner) callModel(
	ctx context.Context,
	params RunParams,
	deps RunDependencies,
	request providerapi.Request,
	requestKind hook.ModelRequestKind,
	sequence uint64,
	onEvent func(providerapi.Event),
) (response Response, err error) {
	boundary := hook.ModelRequest{
		Identity: runIdentity(params, request.TurnID),
		Kind:     requestKind,
		Sequence: sequence,
		Request:  request,
	}
	boundary.OperationID = hook.NewOperationID()
	if hookErr := r.HookDispatcher.BeforeModelRequest(ctx, boundary); hookErr != nil {
		return response, NewFailure(FailureInternal, hookErr)
	}
	if requestKind == hook.ModelRequestIteration {
		request, err = remindContextBudget(request)
		if err != nil {
			return response, NewFailure(FailureContract, err)
		}
	}
	if err := ctx.Err(); err != nil {
		return response, err
	}
	var span *timing.Span
	if requestKind == hook.ModelRequestIteration {
		span = timing.NewSpan()
	}
	if err := deps.Progress.RecordModelRequest(ctx, sequence, agentrun.ModelRequestRecord{
		RunID:  params.ID,
		TurnID: request.TurnID,
		Kind:   requestKind,
		Status: agentrun.ModelRequestRunning,
	}, span.Snapshot()); err != nil {
		return response, err
	}
	boundary.Request = request.Clone()
	ctx, scope := r.HookDispatcher.Start(ctx, hook.Operation{
		Identity:    boundary.Identity,
		Kind:        hook.OperationProvider,
		Name:        request.Model.ProviderID + "/" + request.Model.ModelID,
		ReferenceID: "assistant:" + request.TurnID.String(),
	})
	defer func() {
		// 取消或断流也保存已知请求事实，明细与累计快照使用同一次日志提交。
		if progressErr := deps.Progress.RecordModelResponse(context.WithoutCancel(ctx), agentrun.ModelRequestRecord{
			RunID:       params.ID,
			TurnID:      request.TurnID,
			Kind:        requestKind,
			Status:      agentrun.RequestStatus(ctx, err),
			StopReason:  response.StopReason,
			OutputBytes: response.textBytes,
			Usage:       response.Usage,
		}, response.Timing); progressErr != nil {
			err = NewFailure(FailureClassification(progressErr), errors.Join(err, progressErr))
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
		select {
		case <-ctx.Done():
			return response, ctx.Err()
		case event, ok := <-stream:
			if !ok {
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
				if err := ctx.Err(); err != nil {
					return response, err
				}
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
