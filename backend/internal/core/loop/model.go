package loop

import (
	"bytes"
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"praxis/internal/core/agent/execution/timing"
	"praxis/internal/core/hook"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/core/tool"
	"reflect"
	"slices"
)

// modelResponse 保存有序模型内容与原始文本字节数，不包含消息或上下文类型。
type modelResponse struct {
	events     []providerapi.Event
	textBytes  int
	stopReason providerapi.StopReason
}

// requestModel 只发起请求和消费模型事件，不读取 Execution、不记录进度或触发 Hook。
func requestModel(
	ctx context.Context,
	modelProvider providerapi.Provider,
	request providerapi.Request,
	onEvent func(providerapi.Event),
) (modelResponse, error) {
	streamCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	input, err := modelProvider.Stream(streamCtx, request)
	if err != nil {
		return modelResponse{}, err
	}
	response, err := collectModelResponse(streamCtx, input, onEvent)
	if err == nil {
		err = response.bindReasoningState(request.Model)
	}
	return response, err
}

// callModel 在固定位置执行数据扩展、原生进度记录与模型计时。
func (r *Runner) callModel(
	ctx context.Context,
	executionInput Input,
	deps ExecutionDependencies,
	request providerapi.Request,
	requestKind hook.ModelRequestKind,
	sequence uint64,
	onEvent func(providerapi.Event),
) (response modelResponse, err error) {
	boundary := hook.ModelRequest{
		Identity: executionIdentity(executionInput, request.TurnID),
		Kind:     requestKind,
		Sequence: sequence,
		Request:  request,
	}
	boundary.OperationID = hook.NewOperationID()
	modified, hookErr := r.HookDispatcher.BeforeModelRequest(ctx, boundary)
	if hookErr != nil {
		return response, NewFailure(FailureInternal, hookErr)
	}
	if err := validateHookRequest(request, modified); err != nil {
		return response, NewFailure(FailureContract, err)
	}
	request = modified
	if err := ctx.Err(); err != nil {
		return response, err
	}
	if err := deps.Execution.RecordModelRequest(ctx, sequence, requestKind == hook.ModelRequestIteration); err != nil {
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
		// 取消或断流也要将已收到的消耗接纳到内存进度，不依赖落盘完成。
		if progressErr := deps.Execution.RecordModelOutput(context.WithoutCancel(ctx), response.textBytes); progressErr != nil {
			err = deps.Execution.EntryFailure(errors.Join(err, progressErr))
		}
		if hookErr := r.HookDispatcher.AfterModelResponse(context.WithoutCancel(ctx), hook.ModelResponse{
			Request:     boundary,
			OutputBytes: response.textBytes,
			StopReason:  response.stopReason,
			Err:         err,
		}); hookErr != nil {
			r.Logf("after model response hook failed: %v", hookErr)
		}
		scope.End(hook.Result{Err: err, StopReason: response.stopReason})
	}()
	ctx, span := deps.Timing.Start(ctx, timing.Operation{
		ExecutionID: executionInput.ID.String(),
		Kind:        timing.Provider,
		Name:        request.Model.ProviderID + "/" + request.Model.ModelID,
		ReferenceID: "assistant:" + request.TurnID.String(),
	})
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
	span.End(timing.ResultStatus(ctx, err))
	return response, err
}

// validateHookRequest 保留冻结模型、归属及结构化输出约束；扩展只能缩小输出和工具范围。
// 最终编码后的上下文窗口继续由 Provider 原生校验。
func validateHookRequest(original, modified providerapi.Request) error {
	if original.ExecutionID != modified.ExecutionID || original.TurnID != modified.TurnID || original.PromptCacheKey != modified.PromptCacheKey ||
		!reflect.DeepEqual(original.Model, modified.Model) || !reflect.DeepEqual(original.OutputSchema, modified.OutputSchema) {
		return errors.New("model request hook cannot change frozen configuration or identity")
	}
	if modified.MaxOutputTokens <= 0 || modified.MaxOutputTokens > original.MaxOutputTokens {
		return errors.New("model request hook cannot increase the output token limit")
	}
	allowed := make(map[tool.ToolName]tool.ToolDefinition, len(original.Tools))
	for _, definition := range original.Tools {
		allowed[definition.Name] = definition
	}
	for _, definition := range modified.Tools {
		expected, ok := allowed[definition.Name]
		if !ok || !reflect.DeepEqual(expected, definition) {
			return errors.New("model request hook cannot add or redefine tools")
		}
		delete(allowed, definition.Name)
	}
	return nil
}

// collectModelResponse 只消费模型事件流；预算、任务失败分类和实时事件映射由调用方负责。
// 相邻文本增量合并为一个内容事件，用量只实时转发；流失败时返回已收到的部分内容和错误。
func collectModelResponse(
	ctx context.Context,
	stream <-chan providerapi.Event,
	onEvent func(providerapi.Event),
) (modelResponse, error) {
	var response modelResponse
	if stream == nil {
		return response, errors.New("provider returned a nil stream")
	}
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
				if event.Text == "" {
					continue
				}
				if event.Kind == providerapi.StreamTextDelta {
					response.textBytes += len(event.Text)
				}
				last := len(response.events) - 1
				if last >= 0 && response.events[last].Kind == event.Kind && sameOutputIndex(response.events[last].OutputIndex, event.OutputIndex) {
					response.events[last].Text += event.Text
				} else {
					response.events = append(response.events, providerapi.Event{Kind: event.Kind, Text: event.Text, OutputIndex: cloneOutputIndex(event.OutputIndex)})
				}
			case providerapi.StreamToolCall:
				call := event.ToolCall
				call.Arguments = bytes.Clone(call.Arguments)
				response.events = append(response.events, providerapi.Event{Kind: providerapi.StreamToolCall, ToolCall: call, OutputIndex: cloneOutputIndex(event.OutputIndex)})
				// 回调与已收集结果各自拥有参数副本，观察者不能改写待执行调用。
				event.ToolCall = call
				event.ToolCall.Arguments = bytes.Clone(call.Arguments)
			case providerapi.StreamReasoningState:
				if event.ReasoningState == nil {
					return response, errors.New("provider emitted a nil reasoning state")
				}
				response.events = append(response.events, providerapi.Event{
					Kind:           providerapi.StreamReasoningState,
					ReasoningState: event.ReasoningState.Clone(),
					OutputIndex:    cloneOutputIndex(event.OutputIndex),
				})
				// 推理状态仅保存在内部，观察者不接收签名或加密载荷。
				continue
			case providerapi.StreamUsage:
				if event.Usage == nil || !event.Usage.Valid() {
					continue
				}
			case providerapi.StreamComplete:
				if err := ctx.Err(); err != nil {
					return response, err
				}
				response.stopReason = event.StopReason
				indexed := len(response.events) > 0 && response.events[0].OutputIndex != nil
				for _, content := range response.events {
					if (content.OutputIndex != nil) != indexed || content.OutputIndex != nil && *content.OutputIndex < 0 {
						return response, errors.New("provider emitted inconsistent output ordering")
					}
				}
				// 输出块按协议顺序重放；工具事件可在流终态才发送，不改变块的历史位置。
				slices.SortStableFunc(response.events, func(a, b providerapi.Event) int {
					if a.OutputIndex == nil || b.OutputIndex == nil {
						return 0
					}
					return cmp.Compare(*a.OutputIndex, *b.OutputIndex)
				})
				return response, response.validateStopReason()
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

// validateStopReason 在接纳内容或执行工具前检查终态，摘要调用也遵守同一约束。
func (r modelResponse) validateStopReason() error {
	toolCount := 0
	for _, event := range r.events {
		if event.Kind == providerapi.StreamToolCall {
			toolCount++
		}
	}
	switch r.stopReason {
	case providerapi.StopCompleted, providerapi.StopSequence:
		if toolCount != 0 {
			return errors.New("provider completed with unexpected tool calls")
		}
	case providerapi.StopToolCalls:
		if toolCount == 0 {
			return errors.New("provider requested tools without a tool call")
		}
	case providerapi.StopTruncated:
		return errors.New("model output was truncated")
	case providerapi.StopFiltered:
		return errors.New("model output was filtered")
	case providerapi.StopRefused:
		return errors.New("model refused the request")
	case providerapi.StopPaused:
		return errors.New("model paused the turn; continuation is not supported")
	default:
		return fmt.Errorf("provider returned an unknown stop reason %q", r.stopReason)
	}
	return validateToolCalls(r.toolCalls())
}

func (r modelResponse) toolCalls() []ToolCall {
	calls := make([]ToolCall, 0)
	for _, event := range r.events {
		if event.Kind == providerapi.StreamToolCall {
			call := event.ToolCall
			call.Arguments = bytes.Clone(call.Arguments)
			calls = append(calls, call)
		}
	}
	return calls
}

func cloneOutputIndex(index *int) *int {
	if index == nil {
		return nil
	}
	copy := *index
	return &copy
}

func sameOutputIndex(a, b *int) bool {
	return a == nil && b == nil || a != nil && b != nil && *a == *b
}

// bindReasoningState 确认响应协议后绑定冻结的请求身份，不重复解析 Decoder 已处理的载荷。
func (r *modelResponse) bindReasoningState(config modelapi.Config) error {
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
