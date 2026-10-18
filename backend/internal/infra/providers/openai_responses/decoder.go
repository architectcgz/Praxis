package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	appcontext "praxis/internal/core/context"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/infra/providers/streaming"
	"strings"
)

type decoder struct{}
type responsePartKey struct {
	Kind  string
	Index int
}
type responsePartState struct {
	Text       string
	TextDone   bool
	Closed     bool
	Incomplete bool
}
type responseItemState struct {
	Item          responseOutputValue
	Parts         map[responsePartKey]*responsePartState
	Arguments     string
	ArgumentsDone bool
	Completed     bool
	Raw           json.RawMessage
}
type responseDecoderState struct {
	Items      map[int]*responseItemState
	IDs        map[string]int
	Identity   *responseSnapshot
	InProgress bool
	Refused    bool
}

// Decode 按官方具体事件分派，流式状态与最终快照一致后才释放任何工具。
// output_item.added 的加密状态不能续传，原始推理载荷取自 output_item.done。
func (decoder) Decode(ctx context.Context, reader *streaming.SSEReader, emit streaming.EmitFunc) (modelapi.ModelStopReason, error) {
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
		event, err := decodeResponseEvent([]byte(sse.Data))
		if err != nil {
			return "", fmt.Errorf("decode responses event: %w", err)
		}
		header := event.eventHeader()
		if sse.Type != "" && sse.Type != header.Type {
			return "", errors.New("responses event type mismatch")
		}
		if header.SequenceNumber != nil {
			if *header.SequenceNumber <= lastSequence {
				return "", errors.New("responses event sequence is not increasing")
			}
			lastSequence = *header.SequenceNumber
		}
		if e, ok := event.(*responseErrorEvent); ok {
			return "", fmt.Errorf("responses stream error (%v): %s", stringValue(e.Code), e.Message)
		}
		if _, unknown := event.(*responseEventHeader); unknown {
			continue
		}
		if e, ok := event.(*responseCreatedEvent); ok {
			if state.Identity != nil || e.Response.Status == nil || *e.Response.Status != "in_progress" || len(e.Response.Output) != 0 {
				return "", errors.New("responses invalid or duplicate response.created")
			}
			state.Identity = e.Response
			continue
		}
		if state.Identity == nil {
			return "", errors.New("responses event before response.created")
		}
		switch e := event.(type) {
		case *responseInProgressEvent:
			if state.InProgress || state.checkIdentity(e.Response) != nil || e.Response.Status == nil || *e.Response.Status != "in_progress" {
				return "", errors.New("responses invalid response.in_progress")
			}
			state.InProgress = true
		case *responseOutputItemAddedEvent:
			_, err = state.addItem(*e.OutputIndex, e.Item)
		case *responseOutputItemDoneEvent:
			item := state.Items[*e.OutputIndex]
			if item == nil || item.Completed {
				return "", errors.New("responses done for missing or completed item")
			}
			err = state.finishItem(*e.OutputIndex, e.Item, emit)
		case *responseContentPartAddedEvent:
			kind, text := e.Part.Value.partText()
			err = state.addPart(e.responseItemReference, responsePartKey{kind, *e.ContentIndex}, text, emit)
		case *responseContentPartDoneEvent:
			kind, text := e.Part.Value.partText()
			err = state.closePart(e.responseItemReference, responsePartKey{kind, *e.ContentIndex}, text, emit)
		case *responseReasoningSummaryPartAddedEvent:
			err = state.addPart(e.responseItemReference, responsePartKey{"summary_text", *e.SummaryIndex}, e.Part.Text, emit)
		case *responseReasoningSummaryPartDoneEvent:
			key := responsePartKey{"summary_text", *e.SummaryIndex}
			err = state.closePart(e.responseItemReference, key, e.Part.Text, emit)
			if err == nil && e.Status != nil {
				state.Items[*e.OutputIndex].Parts[key].Incomplete = true
			}
		case *responseOutputTextDeltaEvent:
			err = state.textPart(e.responseItemReference, responsePartKey{"output_text", *e.ContentIndex}, e.Delta, false, emit)
		case *responseOutputTextDoneEvent:
			err = state.textPart(e.responseItemReference, responsePartKey{"output_text", *e.ContentIndex}, e.Text, true, emit)
		case *responseRefusalDeltaEvent:
			err = state.textPart(e.responseItemReference, responsePartKey{"refusal", *e.ContentIndex}, e.Delta, false, emit)
		case *responseRefusalDoneEvent:
			err = state.textPart(e.responseItemReference, responsePartKey{"refusal", *e.ContentIndex}, e.Refusal, true, emit)
		case *responseReasoningSummaryTextDeltaEvent:
			err = state.textPart(e.responseItemReference, responsePartKey{"summary_text", *e.SummaryIndex}, e.Delta, false, emit)
		case *responseReasoningSummaryTextDoneEvent:
			err = state.textPart(e.responseItemReference, responsePartKey{"summary_text", *e.SummaryIndex}, e.Text, true, emit)
		case *responseReasoningTextDeltaEvent:
			err = state.textPart(e.responseItemReference, responsePartKey{"reasoning_text", *e.ContentIndex}, e.Delta, false, emit)
		case *responseReasoningTextDoneEvent:
			err = state.textPart(e.responseItemReference, responsePartKey{"reasoning_text", *e.ContentIndex}, e.Text, true, emit)
		case *responseFunctionCallArgumentsDeltaEvent:
			err = state.arguments(e.responseItemReference, e.Delta, false)
		case *responseFunctionCallArgumentsDoneEvent:
			err = state.arguments(e.responseItemReference, e.Arguments, true)
		case *responseCompletedEvent:
			return state.terminal(e.Response, "completed", emit)
		case *responseFailedEvent:
			return state.terminal(e.Response, "failed", emit)
		case *responseIncompleteEvent:
			return state.terminal(e.Response, "incomplete", emit)
		}
		if err != nil {
			return "", err
		}
	}
}

func stringValue(value *string) string {
	if value == nil {
		return ""
	}
	return *value
}
func (s *responseDecoderState) checkIdentity(response *responseSnapshot) error {
	if response.ID != s.Identity.ID {
		return errors.New("responses conflicting response identity")
	}
	return nil
}
func (s *responseDecoderState) addItem(index int, output responseOutput) (*responseItemState, error) {
	if index < 0 || s.Items[index] != nil {
		return nil, errors.New("responses duplicate or invalid output index")
	}
	id, _ := output.Value.outputIdentity()
	if id != "" {
		if _, exists := s.IDs[id]; exists {
			return nil, errors.New("responses duplicate item ID")
		}
	}
	state := &responseItemState{Item: output.Value, Parts: map[responsePartKey]*responsePartState{}}
	if item, ok := output.Value.(*responseFunctionCallOutput); ok {
		state.Arguments = item.Arguments
	}
	s.Items[index] = state
	if id != "" {
		s.IDs[id] = index
	}
	return state, nil
}
func (s *responseDecoderState) eventItem(ref responseItemReference) (*responseItemState, int, error) {
	index := *ref.OutputIndex
	item := s.Items[index]
	if item == nil || item.Completed {
		return nil, 0, errors.New("responses event references missing or completed item")
	}
	id, _ := item.Item.outputIdentity()
	// function_call.id 可选；未知时只通过已注册的 output_index 关联分片。
	if id != "" && ref.ItemID != id {
		return nil, 0, errors.New("responses conflicting item_id")
	}
	return item, index, nil
}
func (s *responseDecoderState) partItem(ref responseItemReference, key responsePartKey) (*responseItemState, int, error) {
	item, index, err := s.eventItem(ref)
	if err != nil {
		return nil, 0, err
	}
	_, itemType := item.Item.outputIdentity()
	if (key.Kind == "output_text" || key.Kind == "refusal") && itemType != "message" || (key.Kind == "summary_text" || key.Kind == "reasoning_text") && itemType != "reasoning" {
		return nil, 0, errors.New("responses content event targets wrong item type")
	}
	// 同一 content_index 不能先是正文再变成拒绝；summary_index 是独立索引空间。
	for previous := range item.Parts {
		if previous.Index == key.Index && previous.Kind != key.Kind && (previous.Kind == "summary_text") == (key.Kind == "summary_text") {
			return nil, 0, errors.New("responses conflicting content part type")
		}
	}
	return item, index, nil
}
func (s *responseDecoderState) addPart(ref responseItemReference, key responsePartKey, text string, emit streaming.EmitFunc) error {
	item, index, err := s.partItem(ref, key)
	if err != nil {
		return err
	}
	if item.Parts[key] != nil {
		return errors.New("responses duplicate content part start")
	}
	part := &responsePartState{}
	item.Parts[key] = part
	return s.updatePart(index, key, part, text, false, emit)
}
func (s *responseDecoderState) textPart(ref responseItemReference, key responsePartKey, text string, done bool, emit streaming.EmitFunc) error {
	item, index, err := s.partItem(ref, key)
	if err != nil {
		return err
	}
	part := item.Parts[key]
	if part == nil || part.Closed || part.TextDone {
		return errors.New("responses text event before part start or after text completion")
	}
	if err := s.updatePart(index, key, part, text, done, emit); err != nil {
		return err
	}
	part.TextDone = done
	return nil
}
func (s *responseDecoderState) closePart(ref responseItemReference, key responsePartKey, text string, emit streaming.EmitFunc) error {
	item, index, err := s.partItem(ref, key)
	if err != nil {
		return err
	}
	part := item.Parts[key]
	if part == nil || part.Closed {
		return errors.New("responses duplicate or missing content part stop")
	}
	if err := s.updatePart(index, key, part, text, true, emit); err != nil {
		return err
	}
	part.TextDone, part.Closed = true, true
	return nil
}
func (s *responseDecoderState) updatePart(index int, key responsePartKey, part *responsePartState, text string, snapshot bool, emit streaming.EmitFunc) error {
	kind := modelapi.StreamThinkingDelta
	switch key.Kind {
	case "output_text":
		kind = modelapi.StreamTextDelta
	case "refusal":
		kind = modelapi.StreamTextDelta
		s.Refused = true
	case "summary_text", "reasoning_text":
	default:
		return fmt.Errorf("responses unsupported content part %q", key.Kind)
	}
	if snapshot {
		if !strings.HasPrefix(text, part.Text) || part.TextDone && text != part.Text {
			return errors.New("responses inconsistent content snapshot")
		}
		text = text[len(part.Text):]
	}
	if text != "" {
		if err := emit(modelapi.ModelStreamEvent{Kind: kind, Text: text, OutputIndex: &index}); err != nil {
			return err
		}
		part.Text += text
	}
	return nil
}
func (s *responseDecoderState) arguments(ref responseItemReference, text string, done bool) error {
	item, _, err := s.eventItem(ref)
	if err != nil {
		return err
	}
	if _, ok := item.Item.(*responseFunctionCallOutput); !ok || item.ArgumentsDone {
		return errors.New("responses invalid function argument event")
	}
	if done {
		if !strings.HasPrefix(text, item.Arguments) {
			return errors.New("responses inconsistent arguments snapshot")
		}
		item.Arguments, item.ArgumentsDone = text, true
	} else {
		item.Arguments += text
	}
	return nil
}

func (s *responseDecoderState) finishItem(index int, output responseOutput, emit streaming.EmitFunc) error {
	state := s.Items[index]
	beforeID, beforeType := state.Item.outputIdentity()
	id, itemType := output.Value.outputIdentity()
	if itemType != beforeType || beforeID != "" && id != "" && id != beforeID {
		return errors.New("responses conflicting item snapshot identity")
	}
	if id != "" {
		if previous, exists := s.IDs[id]; exists && previous != index {
			return errors.New("responses duplicate item ID")
		}
		s.IDs[id] = index
	}
	parts := map[responsePartKey]string{}
	var orderedKeys []responsePartKey
	switch item := output.Value.(type) {
	case *responseMessageOutput:
		if item.Role != state.Item.(*responseMessageOutput).Role {
			return errors.New("responses conflicting message role")
		}
		if item.Status == "in_progress" {
			return errors.New("responses done item still in progress")
		}
		for partIndex, part := range item.Content {
			kind, text := part.Value.partText()
			key := responsePartKey{kind, partIndex}
			parts[key] = text
			orderedKeys = append(orderedKeys, key)
		}
	case *responseReasoningOutput:
		if stringValue(item.Status) == "in_progress" {
			return errors.New("responses done reasoning still in progress")
		}
		for partIndex, part := range item.Summary {
			key := responsePartKey{part.Type, partIndex}
			parts[key] = part.Text
			orderedKeys = append(orderedKeys, key)
		}
		for partIndex, part := range item.Content {
			key := responsePartKey{part.Type, partIndex}
			parts[key] = part.Text
			orderedKeys = append(orderedKeys, key)
		}
	case *responseFunctionCallOutput:
		before := state.Item.(*responseFunctionCallOutput)
		if item.CallID != before.CallID || item.Name != before.Name || stringValue(item.Status) == "in_progress" {
			return errors.New("responses conflicting function identity or status")
		}
		if !strings.HasPrefix(item.Arguments, state.Arguments) || state.ArgumentsDone && item.Arguments != state.Arguments {
			return errors.New("responses conflicting final arguments")
		}
		state.Arguments, state.ArgumentsDone = item.Arguments, true
	}
	for key, part := range state.Parts {
		text, exists := parts[key]
		if !exists || !strings.HasPrefix(text, part.Text) || part.TextDone && text != part.Text {
			return errors.New("responses final item conflicts with streamed part")
		}
	}
	// 快照可以补齐尚未输出的后缀，但不能重复输出已收到的 delta。
	for _, key := range orderedKeys {
		text := parts[key]
		part := state.Parts[key]
		if part == nil {
			part = &responsePartState{}
			state.Parts[key] = part
		}
		if err := s.updatePart(index, key, part, text, true, emit); err != nil {
			return err
		}
		part.TextDone, part.Closed = true, true
	}
	state.Item, state.Completed, state.Raw = output.Value, true, bytes.Clone(output.Raw)
	return nil
}

// validateFinalItem 只比较已消费内容；推理续传始终保留 output_item.done 的原始载荷。
func validateFinalItem(current, final responseOutputValue) error {
	currentID, currentType := current.outputIdentity()
	finalID, finalType := final.outputIdentity()
	if currentType != finalType || currentID != "" && finalID != "" && currentID != finalID {
		return errors.New("responses terminal snapshot conflicts with item identity")
	}
	switch item := current.(type) {
	case *responseMessageOutput:
		snapshot := final.(*responseMessageOutput)
		if len(item.Content) != len(snapshot.Content) {
			return errors.New("responses terminal snapshot conflicts with message content")
		}
		for index, part := range item.Content {
			kind, text := part.Value.partText()
			finalKind, finalText := snapshot.Content[index].Value.partText()
			if kind != finalKind || text != finalText {
				return errors.New("responses terminal snapshot conflicts with message content")
			}
		}
	case *responseFunctionCallOutput:
		snapshot := final.(*responseFunctionCallOutput)
		if item.CallID != snapshot.CallID || item.Name != snapshot.Name || item.Arguments != snapshot.Arguments {
			return errors.New("responses terminal snapshot conflicts with function call")
		}
	}
	return nil
}

func (s *responseDecoderState) terminal(response *responseSnapshot, status string, emit streaming.EmitFunc) (modelapi.ModelStopReason, error) {
	if err := s.checkIdentity(response); err != nil {
		return "", err
	}
	if response.Status == nil || *response.Status != status {
		return "", errors.New("responses inconsistent terminal status")
	}
	for index := range s.Items {
		if index >= len(response.Output) {
			return "", errors.New("responses terminal snapshot is missing an output item")
		}
	}
	for index, output := range response.Output {
		item := s.Items[index]
		if item == nil {
			var err error
			item, err = s.addItem(index, output)
			if err != nil {
				return "", err
			}
		}
		if item.Completed {
			if err := validateFinalItem(item.Item, output.Value); err != nil {
				return "", err
			}
		} else if err := s.finishItem(index, output, emit); err != nil {
			return "", err
		}
	}
	if err := emitResponseUsage(response.Usage, emit); err != nil {
		return "", err
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
			detail = stringValue(response.IncompleteDetails.Reason)
		}
		reason := mapStopReason(status, detail, false, s.Refused)
		if reason == modelapi.StopUnknown {
			return "", fmt.Errorf("responses unknown incomplete reason %q", detail)
		}
		return reason, nil
	}
	if response.Error != nil || response.IncompleteDetails != nil {
		return "", errors.New("responses completed with error or incomplete details")
	}
	return s.complete(response, emit)
}

func (s *responseDecoderState) complete(response *responseSnapshot, emit streaming.EmitFunc) (modelapi.ModelStopReason, error) {
	seenCalls, toolCount := map[string]bool{}, 0
	for index := range response.Output {
		for _, part := range s.Items[index].Parts {
			if part.Incomplete {
				return "", errors.New("responses completed with incomplete summary part")
			}
		}
		switch item := s.Items[index].Item.(type) {
		case *responseMessageOutput:
			if item.Status != "completed" {
				return "", errors.New("responses completed with incomplete message")
			}
		case *responseFunctionCallOutput:
			if item.Status != nil && *item.Status != "completed" {
				return "", errors.New("responses completed with incomplete function")
			}
			if err := streaming.ValidateToolCall(&streaming.ToolAccumulator{ID: item.CallID, Name: item.Name, Arguments: item.Arguments}); err != nil {
				return "", err
			}
			if seenCalls[item.CallID] {
				return "", errors.New("responses duplicate call_id")
			}
			seenCalls[item.CallID] = true
			toolCount++
		case *responseReasoningOutput:
			if item.Status != nil && *item.Status != "completed" {
				return "", errors.New("responses completed with incomplete reasoning")
			}
			if stringValue(item.EncryptedContent) != "" && len(s.Items[index].Raw) > appcontext.MaxReasoningStateBytes {
				return "", errors.New("responses reasoning state payload exceeds size limit")
			}
		}
	}
	if s.Refused {
		return modelapi.StopRefused, nil
	}
	// 全部校验成功后，按 output 顺序发送状态与完整工具，签名不进入展示文本。
	for index := range response.Output {
		switch item := s.Items[index].Item.(type) {
		case *responseReasoningOutput:
			// 摘要可正常展示；只有加密载荷才能作为无状态续传的 ReasoningState。
			if stringValue(item.EncryptedContent) == "" {
				continue
			}
			if err := emit(modelapi.ModelStreamEvent{
				Kind: modelapi.StreamReasoningState,
				ReasoningState: &appcontext.ReasoningState{
					Protocol: string(modelconfig.APIFormatOpenAIResponses),
					Payload:  bytes.Clone(s.Items[index].Raw),
				},
				OutputIndex: &index,
			}); err != nil {
				return "", err
			}
		case *responseFunctionCallOutput:
			if err := streaming.EmitTool(func(event modelapi.ModelStreamEvent) error { event.OutputIndex = &index; return emit(event) }, &streaming.ToolAccumulator{ID: item.CallID, Name: item.Name, Arguments: item.Arguments}); err != nil {
				return "", err
			}
		}
	}
	return mapStopReason("completed", "", toolCount > 0, false), nil
}

func emitResponseUsage(raw *json.RawMessage, emit streaming.EmitFunc) error {
	var reported responseUsage
	if !streaming.DecodeUsage(raw, &reported, string(modelconfig.APIFormatOpenAIResponses)) {
		return nil
	}
	var usage *modelapi.ModelUsage
	if reported.InputTokens != nil && reported.OutputTokens != nil {
		usage = &modelapi.ModelUsage{
			InputTokens:  *reported.InputTokens,
			OutputTokens: reported.OutputTokens,
		}
		if reported.InputTokensDetails != nil {
			usage.CacheReadInputTokens = reported.InputTokensDetails.CachedTokens
		}
	}
	if !streaming.ValidUsage(string(modelconfig.APIFormatOpenAIResponses), usage) {
		return nil
	}
	return emit(modelapi.ModelStreamEvent{Kind: modelapi.StreamUsage, Usage: usage})
}

// mapStopReason 只将官方终态和明确的截断原因映射为内部结束语义。
func mapStopReason(status, incompleteReason string, hasToolCalls, refused bool) modelapi.ModelStopReason {
	switch status {
	case "completed":
		if refused {
			return modelapi.StopRefused
		}
		if hasToolCalls {
			return modelapi.StopToolCalls
		}
		return modelapi.StopCompleted
	case "incomplete":
		switch incompleteReason {
		case "max_output_tokens":
			return modelapi.StopTruncated
		case "content_filter":
			return modelapi.StopFiltered
		}
	}
	return modelapi.StopUnknown
}
