package loop

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"

	"praxis/internal/core/model"
)

// modelResponse 保存有序模型内容与原始文本字节数，不包含消息或上下文类型。
type modelResponse struct {
	events     []model.ModelStreamEvent
	textBytes  int
	stopReason model.ModelStopReason
}

// requestModel 发起一次模型请求并收集 Provider 事件，不读取 Task、不写入消息或上下文。
func requestModel(
	ctx context.Context,
	stream model.ModelStream,
	request model.ModelRequest,
	onEvent func(model.ModelStreamEvent),
) (modelResponse, error) {
	if stream == nil {
		return modelResponse{}, errors.New("provider stream is required")
	}
	input, err := stream.Stream(ctx, request)
	if err != nil {
		return modelResponse{}, err
	}
	return collectModelResponse(ctx, input, onEvent)
}

// collectModelResponse 只消费模型事件流；预算、任务失败分类和实时事件映射由调用方负责。
// 相邻文本增量合并为一个内容事件，用量只实时转发；流失败时返回已收到的部分内容和错误。
func collectModelResponse(
	ctx context.Context,
	stream <-chan model.ModelStreamEvent,
	onEvent func(model.ModelStreamEvent),
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
			case model.StreamTextDelta, model.StreamThinkingDelta:
				if event.Text == "" {
					continue
				}
				if event.Kind == model.StreamTextDelta {
					response.textBytes += len(event.Text)
				}
				last := len(response.events) - 1
				if last >= 0 && response.events[last].Kind == event.Kind {
					response.events[last].Text += event.Text
				} else {
					response.events = append(response.events, model.ModelStreamEvent{Kind: event.Kind, Text: event.Text})
				}
			case model.StreamToolCall:
				call := event.ToolCall.Snapshot()
				response.events = append(response.events, model.ModelStreamEvent{Kind: model.StreamToolCall, ToolCall: call})
				// 回调与已收集结果各自拥有参数副本，观察者不能改写待执行调用。
				event.ToolCall = call
				event.ToolCall.Arguments = bytes.Clone(call.Arguments)
			case model.StreamUsage:
				if event.Usage == nil || !event.Usage.Valid() {
					continue
				}
			case model.StreamComplete:
				response.stopReason = event.StopReason
				return response, response.validateStopReason()
			case model.StreamError:
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

// validateStopReason 在任何内容落库或工具执行前检查终态，摘要调用也遵守同一约束。
func (r modelResponse) validateStopReason() error {
	toolCount := 0
	for _, event := range r.events {
		if event.Kind == model.StreamToolCall {
			toolCount++
		}
	}
	switch r.stopReason {
	case model.StopCompleted:
		if toolCount != 0 {
			return errors.New("provider completed with unexpected tool calls")
		}
	case model.StopToolCalls:
		if toolCount == 0 {
			return errors.New("provider requested tools without a tool call")
		}
	case model.StopTruncated:
		return errors.New("model output was truncated")
	case model.StopFiltered:
		return errors.New("model output was filtered")
	case model.StopRefused:
		return errors.New("model refused the request")
	case model.StopPaused:
		return errors.New("model paused the turn; continuation is not supported")
	case model.StopSequence:
		return errors.New("model stopped at a custom stop sequence")
	default:
		return fmt.Errorf("provider returned an unknown stop reason %q", r.stopReason)
	}
	return nil
}

func (r modelResponse) toolCalls() []ToolCall {
	calls := make([]ToolCall, 0)
	for _, event := range r.events {
		if event.Kind == model.StreamToolCall {
			call := event.ToolCall
			call.Arguments = bytes.Clone(call.Arguments)
			calls = append(calls, call)
		}
	}
	return calls
}
