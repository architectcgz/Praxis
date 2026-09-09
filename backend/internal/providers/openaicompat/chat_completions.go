package openaicompat

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"

	"praxis/internal/providers/streaming"
	runtimecontract "praxis/internal/runtime"
)

type chatCompletionsProtocol struct{}

func (chatCompletionsProtocol) requestPayload(
	snapshot runtimecontract.ExecutionTurnSnapshot,
) ([]byte, string, error) {
	model := snapshot.Model.ModelID
	if model == "" {
		return nil, "", fmt.Errorf("openai-compatible model is required")
	}
	if snapshot.MaxOutputTokens <= 0 {
		return nil, "", fmt.Errorf("openai-compatible max output tokens is required")
	}
	messages := make([]map[string]any, 0, len(snapshot.Messages)+1)
	if strings.TrimSpace(snapshot.SystemPrompt) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": snapshot.SystemPrompt})
	}
	for _, message := range snapshot.Messages {
		messages = append(messages, chatMessage(message))
	}
	body := map[string]any{"model": model, "messages": messages, "stream": true}
	if reasoning := snapshot.Model.ReasoningLevel; reasoning != "" && reasoning != "off" {
		body["reasoning_effort"] = reasoning
	}
	body["max_tokens"] = snapshot.MaxOutputTokens
	if len(snapshot.Tools) > 0 {
		tools := make([]map[string]any, 0, len(snapshot.Tools))
		for _, tool := range snapshot.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": tool.Name, "description": tool.Description, "parameters": json.RawMessage(tool.InputSchema),
			}})
		}
		body["tools"] = tools
	}
	encoded, err := json.Marshal(body)
	return encoded, "/v1/chat/completions", err
}

func (chatCompletionsProtocol) Decode(
	ctx context.Context,
	reader *streaming.SSEReader,
	emit streaming.EmitFunc,
) (string, error) {
	calls := map[int]*toolAccumulator{}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		event, err := reader.Next()
		if err == io.EOF {
			if err := emitChatTools(emit, calls); err != nil {
				return "", err
			}
			return "", nil
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
					Content   string `json:"content"`
					ToolCalls []struct {
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
			return "", fmt.Errorf("decode openai-compatible stream event: %w", err)
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			if err := emit(runtimecontract.ModelStreamEvent{Kind: runtimecontract.StreamTextDelta, Text: choice.Delta.Content}); err != nil {
				return "", err
			}
		}
		for _, delta := range choice.Delta.ToolCalls {
			call := calls[delta.Index]
			if call == nil {
				call = &toolAccumulator{}
				calls[delta.Index] = call
			}
			if delta.ID != "" {
				call.id = delta.ID
			}
			if delta.Function.Name != "" {
				call.name = delta.Function.Name
			}
			call.arguments += delta.Function.Arguments
		}
		if choice.FinishReason != "" {
			if err := emitChatTools(emit, calls); err != nil {
				return "", err
			}
			return choice.FinishReason, nil
		}
	}
}

func emitChatTools(emit streaming.EmitFunc, calls map[int]*toolAccumulator) error {
	indices := make([]int, 0, len(calls))
	for index := range calls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		if err := emitTool(emit, calls[index]); err != nil {
			return err
		}
	}
	return nil
}
