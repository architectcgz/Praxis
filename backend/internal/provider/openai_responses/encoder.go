package openairesponses

import (
	"encoding/json"
	"errors"
	"fmt"
	"praxis/internal/context"
	sessionentry "praxis/internal/entry"
	"praxis/internal/model"
	"praxis/internal/provider"
	"strings"
)

// encodeContext 按原始顺序编码 input items，仅重放与目标模型身份完全一致的推理状态。
// 上下文结构由消息写入与恢复边界保证，此处不复查。
func encodeContext(ctx context.ModelContext, config model.Config) ([]json.RawMessage, error) {
	var input []json.RawMessage
	if ctx.SystemPrompt != "" {
		item, err := json.Marshal(inputMessage{Type: "message", Role: "system", Content: []inputText{{Type: "input_text", Text: ctx.SystemPrompt}}})
		if err != nil {
			return nil, err
		}
		input = append(input, item)
	}
	ids := provider.BuildToolCallIDMap(ctx)
	for index, entry := range ctx.Entries {
		items, err := responseItems(entry, config, ids)
		if err != nil {
			return nil, fmt.Errorf("openai responses context entry %d: %w", index, err)
		}
		input = append(input, items...)
	}
	return input, nil
}

func responseItems(entry *sessionentry.Entry, config model.Config, ids map[string]string) ([]json.RawMessage, error) {
	var items []json.RawMessage
	var text strings.Builder
	messageRole, blocks := entry.Dialogue()
	appendItem := func(item any) error {
		encoded, err := json.Marshal(item)
		if err != nil {
			return err
		}
		items = append(items, encoded)
		return nil
	}
	flushText := func() error {
		if text.Len() == 0 {
			return nil
		}
		// 官方 EasyInputMessage 的历史 assistant 文本同样使用 input_text。
		err := appendItem(inputMessage{Type: "message", Role: string(messageRole), Content: []inputText{{Type: "input_text", Text: text.String()}}})
		text.Reset()
		return err
	}
	for index, block := range blocks {
		if _, thinking := block.(sessionentry.ThinkingBlock); thinking {
			continue
		}
		if _, isText := block.(sessionentry.TextBlock); !isText {
			if err := flushText(); err != nil {
				return nil, err
			}
		}
		switch block := block.(type) {
		case sessionentry.TextBlock:
			if messageRole == sessionentry.RoleTool {
				return nil, errors.New("responses tool role requires tool results")
			}
			text.WriteString(block.Text)
		case sessionentry.ToolCallBlock:
			if messageRole != sessionentry.RoleAssistant {
				return nil, errors.New("responses function call requires assistant role")
			}
			if err := appendItem(inputFunctionCall{
				Type:      "function_call",
				CallID:    ids[block.CallID],
				Name:      string(block.Name),
				Arguments: block.Arguments,
			}); err != nil {
				return nil, err
			}
		case sessionentry.ToolResultBlock:
			if err := appendItem(inputFunctionOutput{
				Type:   "function_call_output",
				CallID: ids[block.CallID],
				Output: block.Text,
			}); err != nil {
				return nil, err
			}
		case sessionentry.ReasoningStateBlock:
			state := block.State
			if !config.CanReplayReasoningState(state) {
				continue
			}
			items = append(items, state.Payload)
		default:
			return nil, fmt.Errorf("context block %d has unsupported type %T", index, block)
		}
	}
	if err := flushText(); err != nil {
		return nil, err
	}
	return items, nil
}

func sensitiveStateValues(ctx context.ModelContext) []string {
	var values []string
	for _, entry := range ctx.Entries {
		_, blocks := entry.Dialogue()
		for _, block := range blocks {
			state, ok := block.(sessionentry.ReasoningStateBlock)
			if !ok {
				continue
			}
			var item struct {
				EncryptedContent string `json:"encrypted_content"`
			}
			if json.Unmarshal(state.State.Payload, &item) == nil && item.EncryptedContent != "" {
				values = append(values, item.EncryptedContent)
			}
		}
	}
	return values
}
