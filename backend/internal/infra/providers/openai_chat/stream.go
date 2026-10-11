package openaichat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"praxis/internal/core/model"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/providers/streaming"
)

type streamDecoder struct{}

func (streamDecoder) Decode(
	ctx context.Context,
	reader *streaming.SSEReader,
	emit streaming.EmitFunc,
) (model.ModelStopReason, error) {
	calls := map[int]*streaming.ToolAccumulator{}
	var stopReason string
	finish := func() (model.ModelStopReason, error) {
		if stopReason == "" {
			return "", errors.New("openai chat stream ended without finish_reason")
		}
		reason := providers.OpenAIChatStopReason(stopReason)
		// 非正常终态的工具参数可能尚未完整，不能交给调用方执行。
		if reason == model.StopCompleted || reason == model.StopToolCalls {
			if err := emitChatTools(emit, calls); err != nil {
				return "", err
			}
		}
		return reason, nil
	}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		event, err := reader.Next()
		if errors.Is(err, io.EOF) {
			// finish_reason 本身是选择项终态；兼容终态后不发送 [DONE] 的服务。
			if stopReason != "" {
				return finish()
			}
			return "", streaming.ErrUnexpectedEOF
		}
		if err != nil {
			return "", err
		}
		data := strings.TrimSpace(event.Data)
		if data == "[DONE]" {
			return finish()
		}
		var chunk struct {
			Usage *struct {
				PromptTokens        *int64 `json:"prompt_tokens"`
				CompletionTokens    *int64 `json:"completion_tokens"`
				PromptTokensDetails *struct {
					CachedTokens *int64 `json:"cached_tokens"`
				} `json:"prompt_tokens_details"`
			} `json:"usage"`
			Choices []struct {
				Delta struct {
					Content          string `json:"content"`
					ReasoningContent string `json:"reasoning_content"`
					ToolCalls        []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			return "", fmt.Errorf("decode openai chat stream event: %w", err)
		}
		if chunk.Usage != nil && chunk.Usage.PromptTokens != nil {
			candidate := &model.ModelUsage{
				InputTokens:  *chunk.Usage.PromptTokens,
				OutputTokens: chunk.Usage.CompletionTokens,
			}
			if chunk.Usage.PromptTokensDetails != nil {
				candidate.CacheReadInputTokens = chunk.Usage.PromptTokensDetails.CachedTokens
			}
			if candidate.Valid() {
				if err := emit(model.ModelStreamEvent{Kind: model.StreamUsage, Usage: candidate}); err != nil {
					return "", err
				}
			}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.ReasoningContent != "" {
			if err := emit(model.ModelStreamEvent{
				Kind: model.StreamThinkingDelta,
				Text: choice.Delta.ReasoningContent,
			}); err != nil {
				return "", err
			}
		}
		if choice.Delta.Content != "" {
			if err := emit(model.ModelStreamEvent{
				Kind: model.StreamTextDelta,
				Text: choice.Delta.Content,
			}); err != nil {
				return "", err
			}
		}
		for _, delta := range choice.Delta.ToolCalls {
			call := calls[delta.Index]
			if call == nil {
				call = &streaming.ToolAccumulator{}
				calls[delta.Index] = call
			}
			if delta.ID != "" {
				call.ID = delta.ID
			}
			if delta.Function.Name != "" {
				call.Name = delta.Function.Name
			}
			call.Arguments += delta.Function.Arguments
		}
		if choice.FinishReason != "" {
			stopReason = choice.FinishReason
		}
	}
}

func emitChatTools(emit streaming.EmitFunc, calls map[int]*streaming.ToolAccumulator) error {
	indices := make([]int, 0, len(calls))
	for index := range calls {
		indices = append(indices, index)
	}
	slices.Sort(indices)
	for _, index := range indices {
		if err := streaming.EmitTool(emit, calls[index]); err != nil {
			return err
		}
	}
	return nil
}
