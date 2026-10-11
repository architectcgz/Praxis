package anthropicmessages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"

	"praxis/internal/contracts"
	"praxis/internal/core/model"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/providers/streaming"
)

type decoder struct{}
type contentBlockState struct {
	Block            anthropicContentBlock
	Raw              map[string]json.RawMessage
	Arguments        string
	HasArgumentDelta bool
	Closed           bool
	State            *contracts.ProviderState
}

// Decode 跟踪每个 content block 的完整生命周期；签名和 redacted 状态仅内部保存。
// 工具参数在块关闭时校验，但只有 message_stop 的正常工具终态才发送调用。
func (decoder) Decode(ctx context.Context, reader *streaming.SSEReader, emit streaming.EmitFunc) (model.ModelStopReason, error) {
	blocks := map[int]*contentBlockState{}
	started, stopReason := false, ""
	var usage *anthropicMessageUsage
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
		var event anthropicStreamEvent
		if err := json.Unmarshal([]byte(sse.Data), &event); err != nil {
			return "", fmt.Errorf("decode anthropic stream event: %w", err)
		}
		if event.Type == "" || sse.Type != "" && sse.Type != event.Type {
			return "", errors.New("anthropic event type mismatch")
		}
		switch event.Type {
		case "ping":
			continue
		case "error":
			if event.Error == nil {
				return "", errors.New("anthropic error event missing details")
			}
			return "", fmt.Errorf("anthropic stream error (%s): %s", event.Error.Type, event.Error.Message)
		case "message_start":
			if started || event.Message == nil || event.Message.ID == "" || event.Message.Role != "assistant" || len(event.Message.Content) != 0 {
				return "", errors.New("anthropic invalid message_start")
			}
			started, usage = true, event.Message.Usage
			if err := emitMessageUsage(emit, usage); err != nil {
				return "", err
			}
		case "content_block_start":
			if !started || stopReason != "" || event.Index == nil || *event.Index < 0 || blocks[*event.Index] != nil {
				return "", errors.New("anthropic invalid or duplicate block start")
			}
			var block anthropicContentBlock
			if err := json.Unmarshal(event.ContentBlock, &block); err != nil {
				return "", err
			}
			state := &contentBlockState{Block: block}
			if err := json.Unmarshal(event.ContentBlock, &state.Raw); err != nil {
				return "", err
			}
			index := *event.Index
			switch block.Type {
			case "text", "thinking":
				text, kind := block.Text, model.StreamTextDelta
				if block.Type == "thinking" {
					text, kind = block.Thinking, model.StreamThinkingDelta
				}
				if text != "" {
					if err := emit(model.ModelStreamEvent{Kind: kind, Text: text, OutputIndex: &index}); err != nil {
						return "", err
					}
				}
			case "redacted_thinking":
				if block.Data == "" {
					return "", errors.New("anthropic redacted_thinking missing data")
				}
			case "tool_use":
				state.Arguments = string(block.Input)
			default:
				return "", fmt.Errorf("anthropic unsupported content block %q", block.Type)
			}
			blocks[index] = state
		case "content_block_delta":
			state, index, err := activeContentBlock(blocks, event.Index)
			if err != nil {
				return "", err
			}
			delta := event.Delta
			kind, text := model.ModelStreamEventKind(""), ""
			switch delta.Type {
			case "text_delta":
				if state.Block.Type != "text" {
					return "", errors.New("anthropic text delta targets wrong block")
				}
				state.Block.Text += delta.Text
				kind, text = model.StreamTextDelta, delta.Text
			case "thinking_delta":
				if state.Block.Type != "thinking" {
					return "", errors.New("anthropic thinking delta targets wrong block")
				}
				state.Block.Thinking += delta.Thinking
				kind, text = model.StreamThinkingDelta, delta.Thinking
			case "signature_delta":
				if state.Block.Type != "thinking" {
					return "", errors.New("anthropic signature delta targets wrong block")
				}
				state.Block.Signature += delta.Signature
			case "input_json_delta":
				if state.Block.Type != "tool_use" {
					return "", errors.New("anthropic input delta targets wrong block")
				}
				if !state.HasArgumentDelta {
					// 官方 start 的空对象是分片占位符；不能把它拼到后续 JSON 前面。
					var initial map[string]json.RawMessage
					if json.Unmarshal(state.Block.Input, &initial) != nil || initial == nil || len(initial) != 0 {
						return "", errors.New("anthropic input delta has conflicting initial input")
					}
					state.Arguments, state.HasArgumentDelta = "", true
				}
				state.Arguments += delta.PartialJSON
			default:
				continue
			}
			if text != "" {
				if err := emit(model.ModelStreamEvent{Kind: kind, Text: text, OutputIndex: &index}); err != nil {
					return "", err
				}
			}
		case "content_block_stop":
			state, _, err := activeContentBlock(blocks, event.Index)
			if err != nil {
				return "", err
			}
			if state.Block.Type == "tool_use" {
				if err := streaming.ValidateToolCall(&streaming.ToolAccumulator{ID: state.Block.ID, Name: state.Block.Name, Arguments: state.Arguments}); err != nil {
					return "", err
				}
			}
			if state.Block.Type == "thinking" || state.Block.Type == "redacted_thinking" {
				if state.Block.Type == "thinking" {
					state.Raw["thinking"], _ = json.Marshal(state.Block.Thinking)
					state.Raw["signature"], _ = json.Marshal(state.Block.Signature)
				}
				payload, err := json.Marshal(state.Raw)
				if err != nil {
					return "", err
				}
				state.State = &contracts.ProviderState{Protocol: contracts.ProtocolAnthropicMessages, Version: contracts.ProviderStateVersion, Payload: payload}
				if err := state.State.Validate(); err != nil {
					return "", err
				}
			}
			state.Closed = true
		case "message_delta":
			if !started {
				return "", errors.New("anthropic message_delta before message_start")
			}
			for _, block := range blocks {
				if !block.Closed {
					return "", errors.New("anthropic message_delta before block stop")
				}
			}
			if event.Delta.StopReason != "" {
				if stopReason != "" {
					return "", errors.New("anthropic duplicate stop_reason")
				}
				stopReason = event.Delta.StopReason
			}
			if event.Usage != nil {
				usage, err = mergeMessageUsage(usage, event.Usage)
				if err != nil {
					return "", err
				}
				if err := emitMessageUsage(emit, usage); err != nil {
					return "", err
				}
			}
		case "message_stop":
			if !started {
				return "", errors.New("anthropic message_stop before message_start")
			}
			reason := providers.AnthropicStopReason(stopReason)
			if reason == model.StopUnknown {
				return "", fmt.Errorf("anthropic invalid stop_reason %q", stopReason)
			}
			for index := range len(blocks) {
				if blocks[index] == nil || !blocks[index].Closed {
					return "", errors.New("anthropic missing or unclosed content block")
				}
			}
			if reason != model.StopCompleted && reason != model.StopToolCalls {
				return reason, nil
			}
			seen, toolCount := map[string]bool{}, 0
			for index := range len(blocks) {
				block := blocks[index]
				if block.Block.Type == "thinking" && block.Block.Signature == "" {
					return "", errors.New("anthropic thinking block missing signature")
				}
				if block.Block.Type == "tool_use" {
					if seen[block.Block.ID] {
						return "", errors.New("anthropic duplicate tool_use ID")
					}
					seen[block.Block.ID] = true
					toolCount++
				}
			}
			if reason == model.StopCompleted && toolCount != 0 || reason == model.StopToolCalls && toolCount == 0 {
				return "", errors.New("anthropic tool blocks conflict with stop_reason")
			}
			for index := range len(blocks) {
				block := blocks[index]
				if block.State != nil {
					if err := emit(model.ModelStreamEvent{Kind: model.StreamProtocolState, ProtocolState: block.State.Clone(), OutputIndex: &index}); err != nil {
						return "", err
					}
				}
				if block.Block.Type == "tool_use" {
					if err := streaming.EmitTool(func(event model.ModelStreamEvent) error { event.OutputIndex = &index; return emit(event) }, &streaming.ToolAccumulator{ID: block.Block.ID, Name: block.Block.Name, Arguments: block.Arguments}); err != nil {
						return "", err
					}
				}
			}
			return reason, nil
		default:
			// 未知未来事件不修改现有状态，不用于推断终态或调用完整性。
		}
	}
}

func activeContentBlock(blocks map[int]*contentBlockState, index *int) (*contentBlockState, int, error) {
	if index == nil || *index < 0 || blocks[*index] == nil || blocks[*index].Closed {
		return nil, 0, errors.New("anthropic event references missing or closed block")
	}
	return blocks[*index], *index, nil
}

func mergeMessageUsage(previous, delta *anthropicMessageUsage) (*anthropicMessageUsage, error) {
	var next anthropicMessageUsage
	if previous != nil {
		next = *previous
	}
	before := []*int64{
		next.InputTokens,
		next.OutputTokens,
		next.CacheReadInputTokens,
		next.CacheCreationInputTokens,
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
		if value == nil {
			continue
		}
		if *value < 0 || before[index] != nil && *value < *before[index] {
			return nil, errors.New("anthropic cumulative usage decreased")
		}
		copy := *value
		*targets[index] = &copy
	}
	return &next, nil
}

func emitMessageUsage(emit streaming.EmitFunc, reported *anthropicMessageUsage) error {
	if reported == nil {
		return nil
	}
	usage := normalizeMessageUsage(reported)
	if usage == nil {
		return errors.New("anthropic invalid usage")
	}
	return emit(model.ModelStreamEvent{Kind: model.StreamUsage, Usage: usage})
}

func normalizeMessageUsage(reported *anthropicMessageUsage) *model.ModelUsage {
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
	// 用量事件不可共享后续会修改的计数器；缓存计数计入统一输入分母。
	copyCount := func(value *int64) *int64 {
		if value == nil {
			return nil
		}
		copy := *value
		return &copy
	}
	usage := &model.ModelUsage{
		InputTokens:              total,
		OutputTokens:             copyCount(reported.OutputTokens),
		CacheReadInputTokens:     copyCount(reported.CacheReadInputTokens),
		CacheCreationInputTokens: copyCount(reported.CacheCreationInputTokens),
	}
	if !usage.Valid() {
		return nil
	}
	return usage
}
