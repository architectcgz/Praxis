package anthropicmessages

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"praxis/internal/contracts"
	appcontext "praxis/internal/core/context"
	"praxis/internal/infra/providers/streaming"
)

type anthropicContext struct {
	System   string
	Messages []anthropicMessage
}

// encodeContext 按 block 语义拆分消息；tool_result 仅影响自身角色，不覆盖其他内容。
func encodeContext(ctx appcontext.ModelContext) (anthropicContext, error) {
	if err := ctx.Validate(); err != nil {
		return anthropicContext{}, err
	}
	result := anthropicContext{System: ctx.SystemPrompt}
	for index, entry := range ctx.Entries {
		messages, err := anthropicContextEntry(entry)
		if err != nil {
			return anthropicContext{}, fmt.Errorf("anthropic context entry %d: %w", index, err)
		}
		for _, message := range messages {
			// 相邻同角色消息按官方规则合并，多工具结果必须位于同一个 user message。
			last := len(result.Messages) - 1
			if last >= 0 && result.Messages[last].Role == message.Role {
				result.Messages[last].Content = append(result.Messages[last].Content, message.Content...)
			} else {
				result.Messages = append(result.Messages, message)
			}
		}
	}
	for _, message := range result.Messages {
		if message.Role != "user" {
			continue
		}
		seenOther := false
		for _, raw := range message.Content {
			var block anthropicContentBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				return anthropicContext{}, err
			}
			if block.Type == "tool_result" && seenOther {
				return anthropicContext{}, errors.New("anthropic tool_result must precede user text")
			}
			seenOther = seenOther || block.Type != "tool_result"
		}
	}
	return result, nil
}

func anthropicContextEntry(entry appcontext.ContextEntry) ([]anthropicMessage, error) {
	var messages []anthropicMessage
	for index, block := range entry.Content {
		role := string(entry.Role)
		var content anthropicContentBlock
		var raw json.RawMessage
		switch block.Kind {
		case appcontext.ContextBlockText:
			if entry.Role == appcontext.ContextRoleTool {
				return nil, errors.New("anthropic tool role requires tool results")
			}
			content = anthropicContentBlock{Type: "text", Text: block.Text}
		case appcontext.ContextBlockToolCall:
			if entry.Role != appcontext.ContextRoleAssistant {
				return nil, errors.New("anthropic tool_use requires assistant role")
			}
			if err := streaming.ValidateToolCall(&streaming.ToolAccumulator{ID: block.CallID, Name: block.Name, Arguments: string(block.Input)}); err != nil {
				return nil, err
			}
			content = anthropicContentBlock{
				Type:  "tool_use",
				ID:    block.CallID,
				Name:  block.Name,
				Input: block.Input,
			}
		case appcontext.ContextBlockToolResult:
			role = "user"
			text := block.Text
			content = anthropicContentBlock{
				Type:      "tool_result",
				ToolUseID: block.CallID,
				Content:   &text,
				IsError:   block.IsError,
			}
		case appcontext.ContextBlockOpaqueState:
			state := block.ProviderState
			if err := state.Validate(); err != nil {
				return nil, err
			}
			if state.Protocol != contracts.ProtocolAnthropicMessages {
				return nil, errors.New("cannot replay another protocol's state in anthropic")
			}
			if err := json.Unmarshal(state.Payload, &content); err != nil {
				return nil, err
			}
			if content.Type == "thinking" {
				if content.Signature == "" {
					return nil, errors.New("anthropic thinking state missing signature")
				}
			} else if content.Type != "redacted_thinking" || content.Data == "" {
				return nil, errors.New("invalid anthropic thinking state")
			}
			raw = bytes.Clone(state.Payload)
		default:
			return nil, fmt.Errorf("context block %d has unsupported kind %q", index, block.Kind)
		}
		if role != "user" && role != "assistant" {
			return nil, errors.New("anthropic unsupported message role")
		}
		if raw == nil {
			var err error
			raw, err = json.Marshal(content)
			if err != nil {
				return nil, err
			}
		}
		last := len(messages) - 1
		if last >= 0 && messages[last].Role == role {
			messages[last].Content = append(messages[last].Content, raw)
		} else {
			messages = append(messages, anthropicMessage{Role: role, Content: []json.RawMessage{raw}})
		}
	}
	return messages, nil
}

// validateThinkingContinuation 拒绝缺失推理状态的 legacy 工具续传，不从展示文本伪造签名。
func validateThinkingContinuation(messages []anthropicMessage) error {
	for index := len(messages) - 1; index >= 0; index-- {
		message := messages[index]
		if message.Role != "assistant" {
			continue
		}
		firstType, hasTools := "", false
		for partIndex, raw := range message.Content {
			var block anthropicContentBlock
			if err := json.Unmarshal(raw, &block); err != nil {
				return err
			}
			if partIndex == 0 {
				firstType = block.Type
			}
			hasTools = hasTools || block.Type == "tool_use"
		}
		if hasTools && firstType != "thinking" && firstType != "redacted_thinking" {
			return errors.New("anthropic enabled thinking continuation is missing the leading signed thinking block")
		}
		return nil
	}
	return nil
}

func sensitiveStateValues(ctx appcontext.ModelContext) []string {
	var values []string
	for _, entry := range ctx.Entries {
		for _, block := range entry.Content {
			if block.ProviderState == nil {
				continue
			}
			var content anthropicContentBlock
			if json.Unmarshal(block.ProviderState.Payload, &content) == nil {
				values = append(values, content.Signature, content.Data)
			}
		}
	}
	return values
}
