package openairesponses

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/infra/providers/streaming"
)

// encodeContext 按原始顺序编码 input items，状态只允许由来源协议原样续传。
func encodeContext(ctx appcontext.ModelContext) ([]json.RawMessage, error) {
	if err := ctx.Validate(); err != nil {
		return nil, err
	}
	var input []json.RawMessage
	if ctx.SystemPrompt != "" {
		item, err := json.Marshal(inputMessage{Type: "message", Role: "system", Content: []inputText{{Type: "input_text", Text: ctx.SystemPrompt}}})
		if err != nil {
			return nil, err
		}
		input = append(input, item)
	}
	for index, entry := range ctx.Entries {
		items, err := responseItems(entry)
		if err != nil {
			return nil, fmt.Errorf("openai responses context entry %d: %w", index, err)
		}
		input = append(input, items...)
	}
	return input, nil
}

func responseItems(entry appcontext.ContextEntry) ([]json.RawMessage, error) {
	var items []json.RawMessage
	var text strings.Builder
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
		err := appendItem(inputMessage{Type: "message", Role: string(entry.Role), Content: []inputText{{Type: "input_text", Text: text.String()}}})
		text.Reset()
		return err
	}
	for index, block := range entry.Content {
		if block.Kind != appcontext.ContextBlockText {
			if err := flushText(); err != nil {
				return nil, err
			}
		}
		switch block.Kind {
		case appcontext.ContextBlockText:
			if entry.Role == appcontext.ContextRoleTool {
				return nil, errors.New("responses tool role requires tool results")
			}
			text.WriteString(block.Text)
		case appcontext.ContextBlockToolCall:
			if entry.Role != appcontext.ContextRoleAssistant {
				return nil, errors.New("responses function call requires assistant role")
			}
			if err := streaming.ValidateToolCall(&streaming.ToolAccumulator{ID: block.CallID, Name: block.Name, Arguments: string(block.Input)}); err != nil {
				return nil, err
			}
			if err := appendItem(inputFunctionCall{
				Type:      "function_call",
				CallID:    block.CallID,
				Name:      block.Name,
				Arguments: string(block.Input),
			}); err != nil {
				return nil, err
			}
		case appcontext.ContextBlockToolResult:
			if err := appendItem(inputFunctionOutput{Type: "function_call_output", CallID: block.CallID, Output: block.Text}); err != nil {
				return nil, err
			}
		case appcontext.ContextBlockReasoningState:
			state := block.ReasoningState
			if err := state.Validate(); err != nil {
				return nil, err
			}
			if state.Protocol != contracts.ProtocolOpenAIResponses {
				return nil, errors.New("cannot replay another protocol's state in responses")
			}
			var output responseOutput
			if err := json.Unmarshal(state.Payload, &output); err != nil {
				return nil, err
			}
			item, ok := output.Value.(*responseReasoningOutput)
			if !ok || item.Status != nil && *item.Status != "completed" {
				return nil, errors.New("invalid responses reasoning state")
			}
			// 无状态续传不能用 summary 或仅有服务端 ID 的 item 伪造推理状态。
			if stringValue(item.EncryptedContent) == "" {
				return nil, errors.New("responses stateless reasoning replay requires encrypted_content")
			}
			items = append(items, bytes.Clone(state.Payload))
		default:
			return nil, fmt.Errorf("context block %d has unsupported kind %q", index, block.Kind)
		}
	}
	if err := flushText(); err != nil {
		return nil, err
	}
	return items, nil
}

func sensitiveStateValues(ctx appcontext.ModelContext) []string {
	var values []string
	for _, entry := range ctx.Entries {
		for _, block := range entry.Content {
			if block.ReasoningState == nil {
				continue
			}
			var item struct {
				EncryptedContent string `json:"encrypted_content"`
			}
			if json.Unmarshal(block.ReasoningState.Payload, &item) == nil && item.EncryptedContent != "" {
				values = append(values, item.EncryptedContent)
			}
		}
	}
	return values
}
