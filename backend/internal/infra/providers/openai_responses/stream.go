package openairesponses

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"

	"praxis/internal/agent_runtime"
	"praxis/internal/infra/providers/streaming"
)

type streamDecoder struct{}

type responseReasoningPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseOutputItem struct {
	ID        string                  `json:"id"`
	CallID    string                  `json:"call_id"`
	Name      string                  `json:"name"`
	Arguments string                  `json:"arguments"`
	Type      string                  `json:"type"`
	Summary   []responseReasoningPart `json:"summary"`
	Content   []responseReasoningPart `json:"content"`
}

type reasoningPartKey struct {
	outputIndex int
	kind        string
	partIndex   int
}

func (streamDecoder) Decode(
	ctx context.Context,
	reader *streaming.SSEReader,
	emit streaming.EmitFunc,
) (string, error) {
	calls := map[string]*streaming.ToolAccumulator{}
	reasoning := map[reasoningPartKey]string{}
	emitThinking := func(key reasoningPartKey, text string, snapshot bool) error {
		previous := reasoning[key]
		if snapshot {
			// 最终快照只补已发送前缀的后缀；不一致的快照不能追加成重复内容。
			if !strings.HasPrefix(text, previous) {
				return nil
			}
			text = text[len(previous):]
		}
		if text == "" {
			return nil
		}
		delta := text
		if previous == "" && len(reasoning) > 0 {
			delta = "\n\n" + text
		}
		if err := emit(agentruntime.ModelStreamEvent{
			Kind: agentruntime.StreamThinkingDelta,
			Text: delta,
		}); err != nil {
			return err
		}
		reasoning[key] = previous + text
		return nil
	}
	finishReasoning := func(outputIndex int, item responseOutputItem) error {
		if item.Type != "reasoning" {
			return nil
		}
		parts, kind := item.Content, "content"
		for _, part := range item.Summary {
			if part.Type == "summary_text" && part.Text != "" {
				parts, kind = item.Summary, "summary"
				break
			}
		}
		// 优先补已流出的形式，不能再把另一种形式作为同一思考的另一份快照追加。
		streamedKind := ""
		for key := range reasoning {
			if key.outputIndex != outputIndex {
				continue
			}
			streamedKind = key.kind
			if key.kind == "content" {
				break
			}
		}
		switch streamedKind {
		case "content":
			parts, kind = item.Content, "content"
		case "summary":
			parts, kind = item.Summary, "summary"
		}
		for index, part := range parts {
			if part.Type != "summary_text" && part.Type != "reasoning_text" {
				continue
			}
			if err := emitThinking(reasoningPartKey{outputIndex, kind, index}, part.Text, true); err != nil {
				return err
			}
		}
		return nil
	}
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
			Type         string                `json:"type"`
			Delta        string                `json:"delta"`
			Text         string                `json:"text"`
			OutputIndex  int                   `json:"output_index"`
			SummaryIndex int                   `json:"summary_index"`
			ContentIndex int                   `json:"content_index"`
			Part         responseReasoningPart `json:"part"`
			Item         responseOutputItem    `json:"item"`
			CallID       string                `json:"call_id"`
			Name         string                `json:"name"`
			Arguments    string                `json:"arguments"`
			Response     struct {
				Status string               `json:"status"`
				Output []responseOutputItem `json:"output"`
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
		case "response.output_text.delta":
			if eventPayload.Delta != "" {
				if err := emit(agentruntime.ModelStreamEvent{
					Kind: agentruntime.StreamTextDelta,
					Text: eventPayload.Delta,
				}); err != nil {
					return "", err
				}
			}
		case "response.reasoning_summary_text.delta", "response.reasoning_text.delta", "response.reasoning.delta":
			key := reasoningPartKey{eventPayload.OutputIndex, "content", eventPayload.ContentIndex}
			if eventPayload.Type == "response.reasoning_summary_text.delta" {
				key.kind, key.partIndex = "summary", eventPayload.SummaryIndex
			}
			if err := emitThinking(key, eventPayload.Delta, false); err != nil {
				return "", err
			}
		case "response.reasoning_summary_text.done", "response.reasoning_summary_part.done", "response.reasoning_text.done":
			key := reasoningPartKey{eventPayload.OutputIndex, "summary", eventPayload.SummaryIndex}
			text := eventPayload.Text
			if eventPayload.Type == "response.reasoning_summary_part.done" {
				text = eventPayload.Part.Text
			} else if eventPayload.Type == "response.reasoning_text.done" {
				key.kind, key.partIndex = "content", eventPayload.ContentIndex
			}
			if err := emitThinking(key, text, true); err != nil {
				return "", err
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
			if err := finishReasoning(eventPayload.OutputIndex, eventPayload.Item); err != nil {
				return "", err
			}
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
			for index, item := range eventPayload.Response.Output {
				if err := finishReasoning(index, item); err != nil {
					return "", err
				}
			}
			if err := emitResponseTools(emit, calls); err != nil {
				return "", err
			}
			if reported := eventPayload.Response.Usage; reported != nil && reported.InputTokens != nil {
				usage := &agentruntime.ModelUsage{
					InputTokens:  *reported.InputTokens,
					OutputTokens: reported.OutputTokens,
				}
				if reported.InputTokensDetails != nil {
					usage.CacheReadInputTokens = reported.InputTokensDetails.CachedTokens
				}
				if usage.Valid() {
					if err := emit(agentruntime.ModelStreamEvent{Kind: agentruntime.StreamUsage, Usage: usage}); err != nil {
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
