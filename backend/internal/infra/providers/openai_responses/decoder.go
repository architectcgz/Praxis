package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"reflect"
	"strings"

	"praxis/internal/contracts"
	"praxis/internal/core/model"
	"praxis/internal/infra/providers/streaming"
)

type decoder struct{}

type responsePartKey struct {
	Kind  string
	Index int
}
type responsePartState struct {
	Text string
	Done bool
}
type responseItemState struct {
	Item          responseOutputItem
	Parts         map[responsePartKey]*responsePartState
	Arguments     string
	ArgumentsDone bool
	Completed     bool
	Raw           json.RawMessage
}
type responseDecoderState struct {
	Items      map[int]*responseItemState
	IDs        map[string]int
	ResponseID string
	Refused    bool
}

// Decode 使用 output_index 定位输出，item_id 校验身份；call_id 只用于工具结果关联。
// 只有 response.completed 能释放工具调用，delta/done/最终快照之间必须保持前缀一致。
func (decoder) Decode(ctx context.Context, reader *streaming.SSEReader, emit streaming.EmitFunc) (model.ModelStopReason, error) {
	state := responseDecoderState{Items: map[int]*responseItemState{}, IDs: map[string]int{}}
	lastSequence := int64(-1)
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		sse, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return "", streaming.ErrUnexpectedEOF
		}
		if err != nil {
			return "", err
		}
		var event responseStreamEvent
		if err := json.Unmarshal([]byte(sse.Data), &event); err != nil {
			return "", fmt.Errorf("decode responses event: %w", err)
		}
		if event.Type == "" || sse.Type != "" && sse.Type != event.Type {
			return "", errors.New("responses event type mismatch")
		}
		if event.SequenceNumber != nil {
			if *event.SequenceNumber <= lastSequence {
				return "", errors.New("responses event sequence is not increasing")
			}
			lastSequence = *event.SequenceNumber
		}
		if event.Type == "error" {
			return "", fmt.Errorf("responses stream error (%s): %s", event.Code, event.Message)
		}
		switch event.Type {
		case "response.created", "response.in_progress":
			if event.Response == nil || event.Response.ID == "" {
				return "", errors.New("responses missing response identity")
			}
			if state.ResponseID != "" && state.ResponseID != event.Response.ID {
				return "", errors.New("responses conflicting response identity")
			}
			state.ResponseID = event.Response.ID
		case "response.output_item.added":
			if event.OutputIndex == nil || *event.OutputIndex < 0 {
				return "", errors.New("responses missing output_index")
			}
			if state.Items[*event.OutputIndex] != nil {
				return "", errors.New("responses duplicate output item")
			}
			if _, err := state.addItem(*event.OutputIndex, event.Item); err != nil {
				return "", err
			}
		case "response.output_item.done":
			if event.OutputIndex == nil || *event.OutputIndex < 0 {
				return "", errors.New("responses missing output_index")
			}
			item := state.Items[*event.OutputIndex]
			if item == nil || item.Completed {
				return "", errors.New("responses done for missing or completed item")
			}
			if err := state.finishItem(*event.OutputIndex, event.Item, emit); err != nil {
				return "", err
			}
		case "response.output_text.delta", "response.output_text.done", "response.refusal.delta", "response.refusal.done",
			"response.reasoning_summary_text.delta", "response.reasoning_summary_text.done", "response.reasoning_text.delta", "response.reasoning_text.done":
			item, index, err := state.eventItem(event)
			if err != nil {
				return "", err
			}
			kind, partIndex, text := "output_text", event.ContentIndex, event.Delta
			if strings.HasPrefix(event.Type, "response.reasoning_summary_text.") {
				kind, partIndex = "summary_text", event.SummaryIndex
			}
			if strings.HasPrefix(event.Type, "response.reasoning_text.") {
				kind = "reasoning_text"
			}
			if strings.HasPrefix(event.Type, "response.refusal.") {
				kind = "refusal"
				state.Refused = true
			}
			if partIndex == nil || *partIndex < 0 {
				return "", errors.New("responses missing part index")
			}
			if kind == "output_text" || kind == "refusal" {
				if item.Item.Type != "message" {
					return "", errors.New("responses text delta targets non-message item")
				}
			} else if item.Item.Type != "reasoning" {
				return "", errors.New("responses reasoning delta targets non-reasoning item")
			}
			snapshot := strings.HasSuffix(event.Type, ".done")
			if snapshot {
				text = event.Text
				if kind == "refusal" {
					text = event.Refusal
				}
			}
			if err := state.part(index, responsePartKey{kind, *partIndex}, text, snapshot, emit); err != nil {
				return "", err
			}
		case "response.content_part.added", "response.content_part.done", "response.reasoning_summary_part.added", "response.reasoning_summary_part.done":
			item, index, err := state.eventItem(event)
			if err != nil {
				return "", err
			}
			partIndex := event.ContentIndex
			if strings.HasPrefix(event.Type, "response.reasoning_summary_part.") {
				partIndex = event.SummaryIndex
			}
			if partIndex == nil || *partIndex < 0 {
				return "", errors.New("responses missing part index")
			}
			if item.Item.Type != "message" && item.Item.Type != "reasoning" {
				return "", errors.New("responses content part targets invalid item")
			}
			text := event.Part.Text
			if event.Part.Type == "refusal" {
				state.Refused = true
				text = event.Part.Refusal
			}
			if err := state.part(index, responsePartKey{event.Part.Type, *partIndex}, text, strings.HasSuffix(event.Type, ".done"), emit); err != nil {
				return "", err
			}
		case "response.function_call_arguments.delta", "response.function_call_arguments.done":
			item, _, err := state.eventItem(event)
			if err != nil {
				return "", err
			}
			if item.Item.Type != "function_call" || item.ArgumentsDone {
				return "", errors.New("responses invalid function argument event")
			}
			if strings.HasSuffix(event.Type, ".done") {
				if !strings.HasPrefix(event.Arguments, item.Arguments) {
					return "", errors.New("responses inconsistent arguments snapshot")
				}
				item.Arguments, item.ArgumentsDone = event.Arguments, true
			} else {
				item.Arguments += event.Delta
			}
		case "response.completed", "response.incomplete", "response.failed":
			if event.SequenceNumber == nil {
				return "", errors.New("responses terminal event is missing sequence_number")
			}
			response := event.Response
			status := strings.TrimPrefix(event.Type, "response.")
			if response == nil || response.ID == "" || response.Status != status || state.ResponseID != "" && state.ResponseID != response.ID {
				return "", errors.New("responses inconsistent terminal response")
			}
			if response.Usage != nil && response.Usage.InputTokens != nil {
				usage := &model.ModelUsage{InputTokens: *response.Usage.InputTokens, OutputTokens: response.Usage.OutputTokens}
				if response.Usage.InputTokensDetails != nil {
					usage.CacheReadInputTokens = response.Usage.InputTokensDetails.CachedTokens
				}
				if !usage.Valid() {
					return "", errors.New("responses invalid usage")
				}
				if err := emit(model.ModelStreamEvent{Kind: model.StreamUsage, Usage: usage}); err != nil {
					return "", err
				}
			}
			if status == "failed" {
				if response.Error != nil {
					return "", fmt.Errorf("responses failed (%s): %s", response.Error.Code, response.Error.Message)
				}
				return "", errors.New("responses failed")
			}
			if status == "incomplete" {
				detail := ""
				if response.IncompleteDetails != nil {
					detail = response.IncompleteDetails.Reason
				}
				reason := mapStopReason(status, detail, false, state.Refused)
				if reason == model.StopUnknown {
					return "", fmt.Errorf("responses unknown incomplete reason %q", detail)
				}
				return reason, nil
			}
			return state.complete(response, emit)
		default:
			// 未来事件不推断语义；非官方终态别名也不能结束 HTTP SSE。
			continue
		}
		if event.SequenceNumber == nil {
			return "", errors.New("responses known event is missing sequence_number")
		}
	}
}

func (s *responseDecoderState) addItem(index int, raw json.RawMessage) (*responseItemState, error) {
	var item responseOutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return nil, err
	}
	if item.ID == "" || item.ID != strings.TrimSpace(item.ID) {
		return nil, errors.New("responses missing or non-canonical item ID")
	}
	if _, exists := s.IDs[item.ID]; exists {
		return nil, errors.New("responses duplicate item ID")
	}
	switch item.Type {
	case "message":
		if item.Role != "assistant" {
			return nil, errors.New("responses output message requires assistant role")
		}
	case "reasoning", "function_call":
	default:
		return nil, fmt.Errorf("responses unsupported output item type %q", item.Type)
	}
	state := &responseItemState{Item: item, Parts: map[responsePartKey]*responsePartState{}, Arguments: item.Arguments}
	s.Items[index], s.IDs[item.ID] = state, index
	return state, nil
}

func (s *responseDecoderState) eventItem(event responseStreamEvent) (*responseItemState, int, error) {
	if event.OutputIndex == nil || *event.OutputIndex < 0 {
		return nil, 0, errors.New("responses missing output_index")
	}
	index := *event.OutputIndex
	item := s.Items[index]
	if item == nil || item.Completed || event.ItemID == "" || event.ItemID != item.Item.ID {
		return nil, 0, errors.New("responses delta references missing, completed or conflicting item")
	}
	return item, index, nil
}

func (s *responseDecoderState) part(index int, key responsePartKey, text string, snapshot bool, emit streaming.EmitFunc) error {
	var kind model.ModelStreamEventKind
	switch key.Kind {
	case "output_text", "refusal":
		kind = model.StreamTextDelta
	case "summary_text", "reasoning_text":
		kind = model.StreamThinkingDelta
	default:
		return fmt.Errorf("responses unsupported content part %q", key.Kind)
	}
	item := s.Items[index]
	part := item.Parts[key]
	if part == nil {
		part = &responsePartState{}
		item.Parts[key] = part
	}
	if snapshot {
		if !strings.HasPrefix(text, part.Text) || part.Done && text != part.Text {
			return errors.New("responses inconsistent content snapshot")
		}
		text = text[len(part.Text):]
	} else if part.Done {
		return errors.New("responses delta after content part completion")
	}
	if text != "" {
		if err := emit(model.ModelStreamEvent{Kind: kind, Text: text, OutputIndex: &index}); err != nil {
			return err
		}
		part.Text += text
	}
	part.Done = part.Done || snapshot
	return nil
}

func (s *responseDecoderState) finishItem(index int, raw json.RawMessage, emit streaming.EmitFunc) error {
	state := s.Items[index]
	var item responseOutputItem
	if err := json.Unmarshal(raw, &item); err != nil {
		return err
	}
	if item.ID != state.Item.ID || item.Type != state.Item.Type || item.CallID != state.Item.CallID || item.Name != state.Item.Name || item.Role != state.Item.Role {
		return errors.New("responses conflicting item snapshot identity")
	}
	if item.Status != "" && item.Status != "completed" {
		return errors.New("responses item did not complete")
	}
	if item.Type == "function_call" {
		if !strings.HasPrefix(item.Arguments, state.Arguments) || state.ArgumentsDone && item.Arguments != state.Arguments {
			return errors.New("responses conflicting final arguments")
		}
		state.Arguments, state.ArgumentsDone = item.Arguments, true
	}
	keys := map[responsePartKey]bool{}
	for _, parts := range [][]responseContentPart{item.Content, item.Summary} {
		for partIndex, part := range parts {
			key := responsePartKey{part.Type, partIndex}
			keys[key] = true
			text := part.Text
			if part.Type == "refusal" {
				s.Refused = true
				text = part.Refusal
			}
			if err := s.part(index, key, text, true, emit); err != nil {
				return err
			}
		}
	}
	for key := range state.Parts {
		if !keys[key] {
			return errors.New("responses final item is missing a streamed part")
		}
	}
	state.Item, state.Completed, state.Raw = item, true, bytes.Clone(raw)
	return nil
}

func (s *responseDecoderState) complete(response *responseSnapshot, emit streaming.EmitFunc) (model.ModelStopReason, error) {
	if response.Output == nil {
		return "", errors.New("responses terminal snapshot is missing output")
	}
	for index, raw := range response.Output {
		state := s.Items[index]
		if state == nil {
			var err error
			state, err = s.addItem(index, raw)
			if err != nil {
				return "", err
			}
		}
		if state.Completed {
			var before, after any
			if json.Unmarshal(state.Raw, &before) != nil || json.Unmarshal(raw, &after) != nil || !reflect.DeepEqual(before, after) {
				return "", errors.New("responses terminal snapshot conflicts with completed item")
			}
		} else if err := s.finishItem(index, raw, emit); err != nil {
			return "", err
		}
	}
	if len(s.Items) != len(response.Output) {
		return "", errors.New("responses terminal snapshot is missing an output item")
	}
	if s.Refused {
		return model.StopRefused, nil
	}
	seenCalls := map[string]bool{}
	toolCount := 0
	for index := range response.Output {
		item := s.Items[index].Item
		if item.Type == "function_call" {
			call := &streaming.ToolAccumulator{ID: item.CallID, Name: item.Name, Arguments: item.Arguments}
			if err := streaming.ValidateToolCall(call); err != nil {
				return "", err
			}
			if seenCalls[item.CallID] {
				return "", errors.New("responses duplicate call_id")
			}
			seenCalls[item.CallID] = true
			toolCount++
		}
		if item.Type == "reasoning" {
			state := &contracts.ReasoningState{
				Protocol: contracts.ProtocolOpenAIResponses,
				Version:  contracts.ReasoningStateVersion,
				Payload:  s.Items[index].Raw,
			}
			if err := state.Validate(); err != nil {
				return "", err
			}
		}
	}
	if toolCount > 0 {
		for _, state := range s.Items {
			if state.Item.Type == "reasoning" && state.Item.EncryptedContent == "" {
				return "", errors.New("responses tool continuation requires encrypted reasoning state")
			}
		}
	}
	// 先完成全部协议校验，再按 output 顺序发送可重放状态和完整工具。
	for index := range response.Output {
		item := s.Items[index].Item
		switch item.Type {
		case "reasoning":
			if err := emit(model.ModelStreamEvent{
				Kind: model.StreamReasoningState,
				ReasoningState: &contracts.ReasoningState{
					Protocol: contracts.ProtocolOpenAIResponses,
					Version:  contracts.ReasoningStateVersion,
					Payload:  bytes.Clone(s.Items[index].Raw),
				},
				OutputIndex: &index,
			}); err != nil {
				return "", err
			}
		case "function_call":
			if err := streaming.EmitTool(func(event model.ModelStreamEvent) error { event.OutputIndex = &index; return emit(event) }, &streaming.ToolAccumulator{ID: item.CallID, Name: item.Name, Arguments: item.Arguments}); err != nil {
				return "", err
			}
		}
	}
	return mapStopReason("completed", "", toolCount > 0, false), nil
}

// mapStopReason 结合终态 status、incomplete_details.reason 和输出内容判断结束语义。
// hasToolCalls 表示存在函数调用，refused 表示存在拒绝内容；非已知终态返回 StopUnknown。
func mapStopReason(status, incompleteReason string, hasToolCalls, refused bool) model.ModelStopReason {
	switch status {
	case "completed":
		if refused {
			return model.StopRefused
		}
		if hasToolCalls {
			return model.StopToolCalls
		}
		return model.StopCompleted
	case "incomplete":
		switch incompleteReason {
		case "max_output_tokens":
			return model.StopTruncated
		case "content_filter":
			return model.StopFiltered
		}
	}
	return model.StopUnknown
}
