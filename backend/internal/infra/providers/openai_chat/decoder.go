package openaichat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	modelapi "praxis/internal/core/model"
	"praxis/internal/infra/providers/streaming"
	"slices"
	"strings"
)

type decoder struct{}

// Decode 只支持 choice 0；finish_reason 后继续读取 Usage，必须收到 [DONE] 才成功。
func (decoder) Decode(ctx context.Context, reader *streaming.SSEReader, emit streaming.EmitFunc) (modelapi.ModelStopReason, error) {
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
			reason := mapStopReason(stopReason)
			if reason == modelapi.StopUnknown {
				return "", fmt.Errorf("openai chat invalid finish_reason %q", stopReason)
			}
			if refused && (reason == modelapi.StopCompleted || reason == modelapi.StopToolCalls) {
				return modelapi.StopRefused, nil
			}
			if reason == modelapi.StopToolCalls {
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
			} else if reason == modelapi.StopCompleted && len(calls) > 0 {
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
		} else if identity.ID != chunk.ID {
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
		if err := emitChatUsage(chunk.Usage, emit); err != nil {
			return "", err
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
				if err := emit(modelapi.ModelStreamEvent{Kind: modelapi.StreamTextDelta, Text: *text}); err != nil {
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

func emitChatUsage(raw *json.RawMessage, emit streaming.EmitFunc) error {
	var reported chatCompletionUsage
	if !streaming.DecodeUsage(raw, &reported, "openai_chat") {
		return nil
	}
	var usage *modelapi.ModelUsage
	if reported.PromptTokens != nil && reported.CompletionTokens != nil {
		usage = &modelapi.ModelUsage{
			InputTokens:  *reported.PromptTokens,
			OutputTokens: reported.CompletionTokens,
		}
		if reported.PromptTokensDetails != nil {
			usage.CacheReadInputTokens = reported.PromptTokensDetails.CachedTokens
		}
	}
	if !streaming.ValidUsage("openai_chat", usage) {
		return nil
	}
	return emit(modelapi.ModelStreamEvent{Kind: modelapi.StreamUsage, Usage: usage})
}

// mapStopReason 将 finish_reason 映射为统一结束语义；空值或未知值返回 StopUnknown。
func mapStopReason(reason string) modelapi.ModelStopReason {
	switch reason {
	case "stop":
		return modelapi.StopCompleted
	case "tool_calls":
		return modelapi.StopToolCalls
	case "length":
		return modelapi.StopTruncated
	case "content_filter":
		return modelapi.StopFiltered
	default:
		return modelapi.StopUnknown
	}
}
