package openaichat

import (
	"context"
	"encoding/json"
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
	stopReason, refused := "", false
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
		if data == "[DONE]" {
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
		var chunk chatCompletionChunk
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", fmt.Errorf("decode openai chat stream event: %w", err)
		}
		if chunk.Error != nil {
			return "", fmt.Errorf("openai chat stream error (%s): %s", chunk.Error.Type, chunk.Error.Message)
		}
		if chunk.Usage != nil && chunk.Usage.PromptTokens != nil {
			usage := &model.ModelUsage{InputTokens: *chunk.Usage.PromptTokens, OutputTokens: chunk.Usage.CompletionTokens}
			if chunk.Usage.PromptTokensDetails != nil {
				usage.CacheReadInputTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			if !usage.Valid() {
				return "", errors.New("openai chat invalid usage")
			}
			if err := emit(model.ModelStreamEvent{Kind: model.StreamUsage, Usage: usage}); err != nil {
				return "", err
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if len(chunk.Choices) != 1 || chunk.Choices[0].Index == nil || *chunk.Choices[0].Index != 0 {
			return "", errors.New("openai chat only supports choice index 0")
		}
		choice := chunk.Choices[0]
		if stopReason != "" {
			return "", errors.New("openai chat received choice after finish_reason")
		}
		if len(choice.Delta.FunctionCall) > 0 && strings.TrimSpace(string(choice.Delta.FunctionCall)) != "null" || choice.FinishReason == "function_call" {
			return "", errors.New("openai chat legacy function_call is unsupported")
		}
		if choice.Delta.Role != "" && choice.Delta.Role != "assistant" {
			return "", errors.New("openai chat invalid delta role")
		}
		for _, text := range []string{choice.Delta.Content, choice.Delta.Refusal} {
			if text != "" {
				if err := emit(model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: text}); err != nil {
					return "", err
				}
			}
		}
		refused = refused || choice.Delta.Refusal != ""
		for _, delta := range choice.Delta.ToolCalls {
			if delta.Index == nil || *delta.Index < 0 || delta.Type != "" && delta.Type != "function" {
				return "", errors.New("openai chat invalid tool delta")
			}
			call := calls[*delta.Index]
			if call == nil {
				if delta.Type != "function" {
					return "", errors.New("openai chat missing function tool type")
				}
				call = &streaming.ToolAccumulator{}
				calls[*delta.Index] = call
			}
			if delta.ID != "" {
				if call.ID != "" && call.ID != delta.ID {
					return "", errors.New("openai chat conflicting tool ID")
				}
				call.ID = delta.ID
			}
			call.Name += delta.Function.Name
			call.Arguments += delta.Function.Arguments
		}
		stopReason = choice.FinishReason
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
