package openaichat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"praxis/internal/infra/providers/streaming"
	runtimecontract "praxis/internal/runtime"
)

type streamDecoder struct{}

func (streamDecoder) Decode(
	ctx context.Context,
	reader *streaming.SSEReader,
	emit streaming.EmitFunc,
) (string, error) {
	calls := map[int]*streaming.ToolAccumulator{}
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
			if err := emitChatTools(emit, calls); err != nil {
				return "", err
			}
			return "", nil
		}
		var chunk struct {
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
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.ReasoningContent != "" {
			if err := emit(runtimecontract.ModelStreamEvent{
				Kind: runtimecontract.StreamThinkingDelta,
				Text: choice.Delta.ReasoningContent,
			}); err != nil {
				return "", err
			}
		}
		if choice.Delta.Content != "" {
			if err := emit(runtimecontract.ModelStreamEvent{
				Kind: runtimecontract.StreamTextDelta,
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
			if err := emitChatTools(emit, calls); err != nil {
				return "", err
			}
			return choice.FinishReason, nil
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
