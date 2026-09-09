package openaicompat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"praxis/internal/providers/streaming"
	runtimecontract "praxis/internal/runtime"
)

type responsesProtocol struct{}

func (responsesProtocol) requestPayload(
	snapshot runtimecontract.ExecutionTurnSnapshot,
) ([]byte, string, error) {
	model := snapshot.Model.ModelID
	if model == "" {
		return nil, "", errors.New("openai-compatible model is required")
	}
	if snapshot.MaxOutputTokens <= 0 {
		return nil, "", errors.New("openai-compatible max output tokens is required")
	}
	input := make([]map[string]any, 0, len(snapshot.Messages)+1)
	if strings.TrimSpace(snapshot.SystemPrompt) != "" {
		input = append(input, map[string]any{"role": "system", "content": snapshot.SystemPrompt})
	}
	for _, message := range snapshot.Messages {
		input = append(input, responseItems(message)...)
	}
	body := map[string]any{"model": model, "input": input, "stream": true}
	if reasoning := snapshot.Model.ReasoningLevel; reasoning != "" && reasoning != "off" {
		body["reasoning"] = map[string]string{"effort": reasoning}
	}
	body["max_output_tokens"] = snapshot.MaxOutputTokens
	if len(snapshot.Tools) > 0 {
		tools := make([]map[string]any, 0, len(snapshot.Tools))
		for _, tool := range snapshot.Tools {
			tools = append(tools, map[string]any{
				"type":        "function",
				"name":        tool.Name,
				"description": tool.Description,
				"parameters":  json.RawMessage(tool.InputSchema),
			})
		}
		body["tools"] = tools
	}
	encoded, err := json.Marshal(body)
	return encoded, "/v1/responses", err
}

func (responsesProtocol) Decode(
	ctx context.Context,
	reader *streaming.SSEReader,
	emit streaming.EmitFunc,
) (string, error) {
	calls := map[string]*toolAccumulator{}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		event, err := reader.Next()
		if err == io.EOF {
			if err := emitResponseTools(emit, calls); err != nil {
				return "", err
			}
			return "", nil
		}
		if err != nil {
			return "", err
		}
		var eventPayload struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Item  struct {
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
				Type      string `json:"type"`
			} `json:"item"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Response  struct {
				Status string `json:"status"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(event.Data), &eventPayload); err != nil {
			return "", fmt.Errorf("decode openai responses stream event: %w", err)
		}
		switch eventPayload.Type {
		case "response.output_text.delta":
			if eventPayload.Delta != "" {
				if err := emit(runtimecontract.ModelStreamEvent{Kind: runtimecontract.StreamTextDelta, Text: eventPayload.Delta}); err != nil {
					return "", err
				}
			}
		case "response.output_item.added":
			if eventPayload.Item.Type == "function_call" {
				callID := responseCallID(eventPayload.Item.CallID, eventPayload.Item.ID)
				calls[callID] = &toolAccumulator{id: callID, name: eventPayload.Item.Name}
			}
		case "response.output_item.done":
			if eventPayload.Item.Type == "function_call" {
				callID := responseCallID(eventPayload.Item.CallID, eventPayload.Item.ID)
				call := calls[callID]
				if call == nil {
					call = &toolAccumulator{id: callID, name: eventPayload.Item.Name}
					calls[callID] = call
				}
				if eventPayload.Item.Arguments != "" {
					call.arguments = eventPayload.Item.Arguments
				}
			}
		case "response.function_call_arguments.delta":
			call := calls[eventPayload.CallID]
			if call == nil {
				call = &toolAccumulator{id: eventPayload.CallID, name: eventPayload.Name}
				calls[eventPayload.CallID] = call
			}
			call.arguments += eventPayload.Delta
		case "response.function_call_arguments.done":
			if call := calls[eventPayload.CallID]; call != nil && eventPayload.Arguments != "" {
				call.arguments = eventPayload.Arguments
			}
		case "response.completed", "response.done":
			if err := emitResponseTools(emit, calls); err != nil {
				return "", err
			}
			return eventPayload.Response.Status, nil
		case "error":
			return "", errors.New("openai responses stream returned an error")
		}
	}
}

func responseCallID(callID, itemID string) string {
	if callID != "" {
		return callID
	}
	return itemID
}

func emitResponseTools(emit streaming.EmitFunc, calls map[string]*toolAccumulator) error {
	keys := make([]string, 0, len(calls))
	for key := range calls {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := emitTool(emit, calls[key]); err != nil {
			return err
		}
	}
	return nil
}
