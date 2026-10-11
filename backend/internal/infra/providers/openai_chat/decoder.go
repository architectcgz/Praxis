package openaichat

import (
	"context"
	"errors"
	"fmt"
	"io"
	"maps"
	"slices"
	"strings"

	"praxis/internal/core/model"
	"praxis/internal/infra/providers/streaming"
)

type decoder struct{}

// Decode 只支持 choice 0；finish_reason 后继续读取 Usage，必须收到 [DONE] 才成功。
func (decoder) Decode(ctx context.Context, reader *streaming.SSEReader, emit streaming.EmitFunc) (model.ModelStopReason, error) {
	calls := map[int]*streaming.ToolAccumulator{}
	stopReason, refused, usageSeen := "", false, false
	var identity *chatCompletionChunk
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		event, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return "", streaming.ErrUnexpectedEOF
		}
		if err != nil {
			return "", err
		}
		data := strings.TrimSpace(event.Data)
		if event.Type != "" && event.Type != "message" {
			return "", errors.New("openai chat unexpected SSE event type")
		}
		if data == "[DONE]" {
			if !usageSeen {
				return "", errors.New("openai chat missing final usage chunk")
			}
			reason := mapStopReason(stopReason)
			if reason == model.StopUnknown {
				return "", fmt.Errorf("openai chat invalid finish_reason %q", stopReason)
			}
			if refused && (reason == model.StopCompleted || reason == model.StopToolCalls) {
				return model.StopRefused, nil
			}
			if reason == model.StopToolCalls {
				if len(calls) == 0 {
					return "", errors.New("openai chat tool_calls finish without calls")
				}
				seen := map[string]bool{}
				indices := slices.Sorted(maps.Keys(calls))
				// 全部校验成功后才发送任何调用，避免部分有效调用被提前执行。
				for position, index := range indices {
					call := calls[index]
					if position != index || seen[call.ID] {
						return "", errors.New("openai chat invalid tool index or duplicate ID")
					}
					if err := streaming.ValidateToolCall(call); err != nil {
						return "", err
					}
					seen[call.ID] = true
				}
				for _, index := range indices {
					if err := streaming.EmitTool(emit, calls[index]); err != nil {
						return "", err
					}
				}
			} else if reason == model.StopCompleted && len(calls) > 0 {
				return "", errors.New("openai chat tool calls conflict with finish_reason")
			}
			return reason, nil
		}
		chunk, err := decodeChunk([]byte(data))
		if err != nil {
			return "", fmt.Errorf("decode openai chat stream event: %w", err)
		}
		if identity == nil {
			identity = &chunk
		} else if identity.ID != chunk.ID || identity.Model != chunk.Model || *identity.Created != *chunk.Created {
			return "", errors.New("openai chat conflicting chunk identity")
		}
		if usageSeen {
			return "", errors.New("openai chat chunk after final usage")
		}
		if len(chunk.Choices) == 0 {
			if chunk.Usage == nil || stopReason == "" {
				return "", errors.New("openai chat invalid usage chunk order")
			}
			usageSeen = true
		} else if chunk.Usage != nil {
			return "", errors.New("openai chat usage requires empty choices")
		}
		if chunk.Usage != nil {
			usage := &model.ModelUsage{InputTokens: *chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
			if chunk.Usage.PromptTokensDetails != nil {
				usage.CacheReadInputTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			if err := emit(model.ModelStreamEvent{Kind: model.StreamUsage, Usage: usage}); err != nil {
				return "", err
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if stopReason != "" {
			return "", errors.New("openai chat received choice after finish_reason")
		}
		for _, text := range []*string{choice.Delta.Content, choice.Delta.Refusal} {
			if text != nil && *text != "" {
				if err := emit(model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: *text}); err != nil {
					return "", err
				}
			}
		}
		refused = refused || choice.Delta.Refusal != nil && *choice.Delta.Refusal != ""
		for _, delta := range choice.Delta.ToolCalls {
			call := calls[*delta.Index]
			if call == nil {
				call = &streaming.ToolAccumulator{}
				calls[*delta.Index] = call
			}
			if delta.ID != nil {
				if call.ID != "" && call.ID != *delta.ID {
					return "", errors.New("openai chat conflicting tool ID")
				}
				call.ID = *delta.ID
			}
			if delta.Function != nil {
				if delta.Function.Name != nil {
					call.Name += *delta.Function.Name
				}
				if delta.Function.Arguments != nil {
					call.Arguments += *delta.Function.Arguments
				}
			}
		}
		if choice.FinishReason != nil {
			stopReason = *choice.FinishReason
		}
	}
}

// mapStopReason 将 finish_reason 映射为统一结束语义；空值或未知值返回 StopUnknown。
func mapStopReason(reason string) model.ModelStopReason {
	switch reason {
	case "stop":
		return model.StopCompleted
	case "tool_calls":
		return model.StopToolCalls
	case "length":
		return model.StopTruncated
	case "content_filter":
		return model.StopFiltered
	default:
		return model.StopUnknown
	}
}
