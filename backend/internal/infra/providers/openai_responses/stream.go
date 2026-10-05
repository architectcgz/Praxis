package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"

	runtimecontract "praxis/internal/agent_runtime"
	"praxis/internal/infra/providers/streaming"
)

type streamDecoder struct{}

func (streamDecoder) Decode(
	ctx context.Context,
	reader *streaming.SSEReader,
	emit streaming.EmitFunc,
) (string, error) {
	calls := map[string]*streaming.ToolAccumulator{}
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
				Usage  *struct {
					InputTokens        *int64 `json:"input_tokens"`
					OutputTokens       *int64 `json:"output_tokens"`
					InputTokensDetails *struct {
						CachedTokens *int64 `json:"cached_tokens"`
					} `json:"input_tokens_details"`
				} `json:"usage"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(event.Data), &eventPayload); err != nil {
			return "", fmt.Errorf("decode openai responses stream event: %w", err)
		}
		switch eventPayload.Type {
		case "response.output_text.delta", "response.reasoning_summary_text.delta", "response.reasoning.delta":
			if eventPayload.Delta != "" {
				kind := runtimecontract.StreamTextDelta
				if eventPayload.Type == "response.reasoning_summary_text.delta" {
					kind = runtimecontract.StreamThinkingDelta
				}
				if err := emit(runtimecontract.ModelStreamEvent{
					Kind: kind,
					Text: eventPayload.Delta,
				}); err != nil {
					return "", err
				}
			}
		case "response.output_item.added":
			if eventPayload.Item.Type == "function_call" {
				callID := responseCallID(eventPayload.Item.CallID, eventPayload.Item.ID)
				calls[callID] = &streaming.ToolAccumulator{
					ID:   callID,
					Name: eventPayload.Item.Name,
				}
			}
		case "response.output_item.done":
			if eventPayload.Item.Type == "function_call" {
				callID := responseCallID(eventPayload.Item.CallID, eventPayload.Item.ID)
				call := calls[callID]
				if call == nil {
					call = &streaming.ToolAccumulator{ID: callID, Name: eventPayload.Item.Name}
					calls[callID] = call
				}
				if eventPayload.Item.Arguments != "" {
					call.Arguments = eventPayload.Item.Arguments
				}
			}
		case "response.function_call_arguments.delta":
			call := calls[eventPayload.CallID]
			if call == nil {
				call = &streaming.ToolAccumulator{ID: eventPayload.CallID, Name: eventPayload.Name}
				calls[eventPayload.CallID] = call
			}
			call.Arguments += eventPayload.Delta
		case "response.function_call_arguments.done":
			if call := calls[eventPayload.CallID]; call != nil && eventPayload.Arguments != "" {
				call.Arguments = eventPayload.Arguments
			}
		case "response.completed", "response.done":
			if err := emitResponseTools(emit, calls); err != nil {
				return "", err
			}
			if reported := eventPayload.Response.Usage; reported != nil && reported.InputTokens != nil {
				usage := &runtimecontract.ModelUsage{
					InputTokens:  *reported.InputTokens,
					OutputTokens: reported.OutputTokens,
				}
				if reported.InputTokensDetails != nil {
					usage.CacheReadInputTokens = reported.InputTokensDetails.CachedTokens
				}
				if usage.Valid() {
					if err := emit(runtimecontract.ModelStreamEvent{Kind: runtimecontract.StreamUsage, Usage: usage}); err != nil {
						return "", err
					}
				}
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

func emitResponseTools(emit streaming.EmitFunc, calls map[string]*streaming.ToolAccumulator) error {
	keys := make([]string, 0, len(calls))
	for key := range calls {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		if err := streaming.EmitTool(emit, calls[key]); err != nil {
			return err
		}
	}
	return nil
}
