package anthropicmessages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/infra/providers/streaming"
)

type decoder struct{}
type contentBlockState struct {
	Block            contentBlock
	Raw              map[string]json.RawMessage
	Arguments        string
	HasArgumentDelta bool
	Signed           bool
	Closed           bool
	ReasoningState   *sessionentry.ReasoningState
}

// Decode 按事件、Content Block 和 Delta 三层类型分派；工具只在 message_stop 后释放。
// 签名和 redacted 数据只内部保存，推理续传不重建或丢弃官方附加字段。
func (decoder) Decode(ctx context.Context, reader *streaming.SSEReader, emit streaming.EmitFunc) (modelapi.ModelStopReason, error) {
	blocks := map[int]*contentBlockState{}
	started, ending, stopReason := false, false, ""
	var usage *tokenCounts
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
		event, err := decodeMessageEvent([]byte(sse.Data))
		if err != nil {
			return "", fmt.Errorf("decode anthropic stream event: %w", err)
		}
		if sse.Type != "" && sse.Type != event.eventType() {
			return "", errors.New("anthropic event type mismatch")
		}
		switch e := event.(type) {
		case *pingEvent, *messageEventHeader:
			continue
		case *errorEvent:
			return "", fmt.Errorf("anthropic stream error (%s): %s", e.Error.Type, e.Error.Message)
		case *messageStartEvent:
			if started || len(e.Message.Content) != 0 || e.Message.StopReason != nil || e.Message.StopSequence != nil {
				return "", errors.New("anthropic invalid or duplicate message_start")
			}
			started = true
			usage, err = updateMessageUsage(usage, e.Message.Usage, emit)
			if err != nil {
				return "", err
			}
		case *contentBlockStartEvent:
			if !started || ending || *e.Index != len(blocks) || blocks[*e.Index] != nil {
				return "", errors.New("anthropic invalid or duplicate block start")
			}
			index := *e.Index
			state := &contentBlockState{Block: e.ContentBlock.Value, Raw: e.ContentBlock.Raw}
			text, kind := "", modelapi.StreamTextDelta
			switch block := state.Block.(type) {
			case *textBlock:
				text = block.Text
			case *thinkingBlock:
				text, kind, state.Signed = block.Thinking, modelapi.StreamThinkingDelta, block.Signature != ""
			case *toolUseBlock:
				state.Arguments = string(block.Input)
			}
			if text != "" {
				if err := emit(modelapi.ModelStreamEvent{Kind: kind, Text: text, OutputIndex: &index}); err != nil {
					return "", err
				}
			}
			blocks[index] = state
		case *contentBlockDeltaEvent:
			state, index, err := activeContentBlock(blocks, e.Index)
			if err != nil {
				return "", err
			}
			if ending {
				return "", errors.New("anthropic content delta after message_delta")
			}
			text, kind := "", modelapi.StreamTextDelta
			switch delta := e.Delta.Value.(type) {
			case *textDelta:
				block, ok := state.Block.(*textBlock)
				if !ok {
					return "", errors.New("anthropic text delta targets wrong block")
				}
				block.Text += delta.Text
				text = delta.Text
			case *thinkingDelta:
				block, ok := state.Block.(*thinkingBlock)
				if !ok || state.Signed {
					return "", errors.New("anthropic thinking delta targets wrong or signed block")
				}
				block.Thinking += delta.Thinking
				text, kind = delta.Thinking, modelapi.StreamThinkingDelta
			case *signatureDelta:
				block, ok := state.Block.(*thinkingBlock)
				if !ok {
					return "", errors.New("anthropic signature delta targets wrong block")
				}
				block.Signature += delta.Signature
				state.Signed = true
			case *inputJSONDelta:
				block, ok := state.Block.(*toolUseBlock)
				if !ok {
					return "", errors.New("anthropic input delta targets wrong block")
				}
				if !state.HasArgumentDelta {
					// 官方 start 的空对象只是占位符，不能拼到后续 JSON 前面。
					var initial map[string]json.RawMessage
					if json.Unmarshal(block.Input, &initial) != nil || initial == nil || len(initial) != 0 {
						return "", errors.New("anthropic input delta has conflicting initial input")
					}
					state.Arguments, state.HasArgumentDelta = "", true
				}
				state.Arguments += delta.PartialJSON
			case *citationsDelta:
				if _, ok := state.Block.(*textBlock); !ok {
					return "", errors.New("anthropic citation delta targets wrong block")
				}
				// 当前仅消费文本，引用元数据不改变正文与推理续传载荷。
			default:
				return "", errors.New("anthropic unsupported content delta")
			}
			if text != "" {
				if err := emit(modelapi.ModelStreamEvent{Kind: kind, Text: text, OutputIndex: &index}); err != nil {
					return "", err
				}
			}
		case *contentBlockStopEvent:
			state, _, err := activeContentBlock(blocks, e.Index)
			if err != nil {
				return "", err
			}
			if ending {
				return "", errors.New("anthropic block stop after message_delta")
			}
			if block, ok := state.Block.(*thinkingBlock); ok {
				state.Raw["thinking"], _ = json.Marshal(block.Thinking)
				state.Raw["signature"], _ = json.Marshal(block.Signature)
			}
			if state.Block.blockType() == "thinking" || state.Block.blockType() == "redacted_thinking" {
				payload, err := json.Marshal(state.Raw)
				if err != nil {
					return "", err
				}
				if len(payload) > sessionentry.MaxReasoningStateBytes {
					return "", errors.New("anthropic reasoning state payload exceeds size limit")
				}
				state.ReasoningState = &sessionentry.ReasoningState{
					Protocol: string(modelconfig.APIFormatAnthropicMessages),
					Payload:  payload,
				}
			}
			state.Closed = true
		case *messageDeltaEvent:
			if !started {
				return "", errors.New("anthropic message_delta before message_start")
			}
			for _, block := range blocks {
				if !block.Closed {
					return "", errors.New("anthropic message_delta before block stop")
				}
			}
			ending = true
			if e.Delta.StopReason != nil {
				if stopReason != "" || mapStopReason(*e.Delta.StopReason) == modelapi.StopUnknown {
					return "", errors.New("anthropic duplicate or invalid stop_reason")
				}
				stopReason = *e.Delta.StopReason
				if (stopReason == "stop_sequence") != (e.Delta.StopSequence != nil) {
					return "", errors.New("anthropic inconsistent stop_sequence")
				}
			} else if e.Delta.StopSequence != nil {
				return "", errors.New("anthropic stop_sequence without stop_reason")
			}
			usage, err = updateMessageUsage(usage, e.Usage, emit)
			if err != nil {
				return "", err
			}
		case *messageStopEvent:
			if !started || !ending {
				return "", errors.New("anthropic message_stop before message_delta")
			}
			reason := mapStopReason(stopReason)
			if reason == modelapi.StopUnknown {
				return "", fmt.Errorf("anthropic invalid stop_reason %q", stopReason)
			}
			for index := range len(blocks) {
				if blocks[index] == nil || !blocks[index].Closed {
					return "", errors.New("anthropic missing or unclosed content block")
				}
			}
			if reason != modelapi.StopCompleted && reason != modelapi.StopSequence && reason != modelapi.StopToolCalls {
				return reason, nil
			}
			seen, toolCount := map[string]bool{}, 0
			for index := range len(blocks) {
				state := blocks[index]
				switch block := state.Block.(type) {
				case *thinkingBlock:
					if block.Signature == "" {
						return "", errors.New("anthropic thinking block missing signature")
					}
				case *toolUseBlock:
					if seen[block.ID] {
						return "", errors.New("anthropic duplicate tool_use ID")
					}
					if err := streaming.ValidateToolCall(&streaming.ToolAccumulator{ID: block.ID, Name: block.Name, Arguments: state.Arguments}); err != nil {
						return "", err
					}
					seen[block.ID] = true
					toolCount++
				}
			}
			if (reason == modelapi.StopCompleted || reason == modelapi.StopSequence) && toolCount != 0 || reason == modelapi.StopToolCalls && toolCount == 0 {
				return "", errors.New("anthropic tool blocks conflict with stop_reason")
			}
			// 所有块、签名与终态都已验证后，再发送任何可执行工具。
			for index := range len(blocks) {
				state := blocks[index]
				if state.ReasoningState != nil {
					if err := emit(modelapi.ModelStreamEvent{Kind: modelapi.StreamReasoningState, ReasoningState: state.ReasoningState.Clone(), OutputIndex: &index}); err != nil {
						return "", err
					}
				}
				if block, ok := state.Block.(*toolUseBlock); ok {
					if err := streaming.EmitTool(func(event modelapi.ModelStreamEvent) error { event.OutputIndex = &index; return emit(event) }, &streaming.ToolAccumulator{ID: block.ID, Name: block.Name, Arguments: state.Arguments}); err != nil {
						return "", err
					}
				}
			}
			return reason, nil
		}
	}
}

func activeContentBlock(blocks map[int]*contentBlockState, index *int) (*contentBlockState, int, error) {
	if index == nil || *index < 0 || blocks[*index] == nil || blocks[*index].Closed {
		return nil, 0, errors.New("anthropic event references missing or closed block")
	}
	return blocks[*index], *index, nil
}

// updateMessageUsage 仅在本次计数完整有效时更新累计值；异常统计不污染后续增量。
func updateMessageUsage(previous *tokenCounts, raw *json.RawMessage, emit streaming.EmitFunc) (*tokenCounts, error) {
	var delta tokenCounts
	if !streaming.DecodeUsage(raw, &delta, string(modelconfig.APIFormatAnthropicMessages)) {
		return previous, nil
	}
	var next tokenCounts
	if previous != nil {
		next = *previous
	}
	after := []*int64{
		delta.InputTokens,
		delta.OutputTokens,
		delta.CacheReadInputTokens,
		delta.CacheCreationInputTokens,
	}
	targets := []**int64{
		&next.InputTokens,
		&next.OutputTokens,
		&next.CacheReadInputTokens,
		&next.CacheCreationInputTokens,
	}
	for index, value := range after {
		if value != nil {
			*targets[index] = value
		}
	}
	var usage *modelapi.ModelUsage
	if delta.OutputTokens != nil {
		usage = normalizeMessageUsage(&next)
	}
	if !streaming.ValidUsage(string(modelconfig.APIFormatAnthropicMessages), usage) {
		return previous, nil
	}
	if err := emit(modelapi.ModelStreamEvent{Kind: modelapi.StreamUsage, Usage: usage}); err != nil {
		return previous, err
	}
	return &next, nil
}
func normalizeMessageUsage(reported *tokenCounts) *modelapi.ModelUsage {
	if reported == nil || reported.InputTokens == nil || *reported.InputTokens < 0 {
		return nil
	}
	total := *reported.InputTokens
	for _, count := range []*int64{reported.CacheReadInputTokens, reported.CacheCreationInputTokens} {
		if count != nil {
			if *count < 0 || *count > math.MaxInt64-total {
				return nil
			}
			total += *count
		}
	}
	// 用量事件不共享后续可变计数器，缓存计数计入统一输入分母。
	copyCount := func(value *int64) *int64 {
		if value == nil {
			return nil
		}
		copy := *value
		return &copy
	}
	usage := &modelapi.ModelUsage{
		InputTokens:              total,
		OutputTokens:             copyCount(reported.OutputTokens),
		CacheReadInputTokens:     copyCount(reported.CacheReadInputTokens),
		CacheCreationInputTokens: copyCount(reported.CacheCreationInputTokens),
	}
	return usage
}

// mapStopReason 保留截断、拒绝和自定义停止序列的独立语义。
func mapStopReason(reason string) modelapi.ModelStopReason {
	switch reason {
	case "end_turn":
		return modelapi.StopCompleted
	case "tool_use":
		return modelapi.StopToolCalls
	case "max_tokens", "model_context_window_exceeded":
		return modelapi.StopTruncated
	case "pause_turn":
		return modelapi.StopPaused
	case "refusal":
		return modelapi.StopRefused
	case "stop_sequence":
		return modelapi.StopSequence
	default:
		return modelapi.StopUnknown
	}
}
