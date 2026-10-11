package anthropicmessages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"slices"
	"strings"

	"praxis/internal/contracts"
	"praxis/internal/core/model"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/providers/streaming"
)

type decoder struct{}

// Decode 将 SSE 转为有序模型事件并返回统一停止原因；协议错误或取消时返回错误。
func (decoder) Decode(
	ctx context.Context,
	reader *streaming.SSEReader,
	emit streaming.EmitFunc,
) (model.ModelStopReason, error) {
	tools := map[int]*toolAccumulator{}
	var usage *model.ModelUsage
	var stopReason string
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		sseEvent, err := reader.Next()
		if errors.Is(err, io.EOF) {
			return "", streaming.ErrUnexpectedEOF
		}
		if err != nil {
			return "", err
		}
		data := strings.TrimSpace(sseEvent.Data)
		var event struct {
			Type  string        `json:"type"`
			Index int           `json:"index"`
			Usage *messageUsage `json:"usage"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Message struct {
				Usage *messageUsage `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return "", fmt.Errorf("decode anthropic stream event: %w", err)
		}
		switch event.Type {
		case "message_start":
			usage = normalizeMessageUsage(event.Message.Usage)
			if usage != nil {
				if err := emit(model.ModelStreamEvent{Kind: model.StreamUsage, Usage: usage}); err != nil {
					return "", err
				}
			}
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				tools[event.Index] = &toolAccumulator{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text != "" {
					if err := emit(model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: event.Delta.Text}); err != nil {
						return "", err
					}
				}
			case "thinking_delta":
				if event.Delta.Thinking != "" {
					if err := emit(model.ModelStreamEvent{Kind: model.StreamThinkingDelta, Text: event.Delta.Thinking}); err != nil {
						return "", err
					}
				}
			case "input_json_delta":
				if tool := tools[event.Index]; tool != nil {
					tool.arguments += event.Delta.PartialJSON
				}
			}
		case "message_delta":
			// message_delta 的输出计数是累计值，保留起始输入计数并替换输出。
			if usage != nil && event.Usage != nil && event.Usage.OutputTokens != nil {
				next := *usage
				next.OutputTokens = event.Usage.OutputTokens
				if next.Valid() && (usage.OutputTokens == nil || *next.OutputTokens >= *usage.OutputTokens) {
					usage = &next
					if err := emit(model.ModelStreamEvent{Kind: model.StreamUsage, Usage: usage}); err != nil {
						return "", err
					}
				}
			}
			if event.Delta.StopReason != "" {
				stopReason = event.Delta.StopReason
			}
		case "message_stop":
			if stopReason == "" {
				return "", errors.New("anthropic stream ended without stop_reason")
			}
			reason := providers.AnthropicStopReason(stopReason)
			if reason == model.StopCompleted || reason == model.StopToolCalls {
				if err := emitTools(emit, tools); err != nil {
					return "", err
				}
			}
			return reason, nil
		case "error":
			return "", errors.New("anthropic stream returned an error")
		}
	}
}

type messageUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
}

func normalizeMessageUsage(reported *messageUsage) *model.ModelUsage {
	if reported == nil || reported.InputTokens == nil || *reported.InputTokens < 0 {
		return nil
	}
	// Anthropic 的 input_tokens 不含缓存读取和写入，统一分母时需补上两者。
	total := *reported.InputTokens
	for _, count := range []*int64{reported.CacheReadInputTokens, reported.CacheCreationInputTokens} {
		if count != nil {
			if *count < 0 || *count > math.MaxInt64-total {
				return nil
			}
			total += *count
		}
	}
	usage := &model.ModelUsage{
		InputTokens:              total,
		OutputTokens:             reported.OutputTokens,
		CacheReadInputTokens:     reported.CacheReadInputTokens,
		CacheCreationInputTokens: reported.CacheCreationInputTokens,
	}
	if !usage.Valid() {
		return nil
	}
	return usage
}

type toolAccumulator struct{ id, name, arguments string }

func emitTool(emit streaming.EmitFunc, tool *toolAccumulator) error {
	if tool == nil || tool.name == "" {
		return errors.New("anthropic emitted an incomplete tool call")
	}
	args := json.RawMessage(tool.arguments)
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	return emit(model.ModelStreamEvent{
		Kind: model.StreamToolCall,
		ToolCall: contracts.ToolCall{
			ID:        tool.id,
			Name:      contracts.ToolName(tool.name),
			Arguments: args,
		},
	})
}

func emitTools(emit streaming.EmitFunc, tools map[int]*toolAccumulator) error {
	for _, index := range slices.Sorted(maps.Keys(tools)) {
		if err := emitTool(emit, tools[index]); err != nil {
			return err
		}
	}
	return nil
}
