package anthropicmessages

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	appcontext "praxis/internal/core/context"
	modelapi "praxis/internal/core/model"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

type anthropicContext struct {
	System   string
	Messages []anthropicMessage
}

// encodeContext 按 block 语义拆分消息；tool_result 仅影响自身角色，不覆盖其他内容。
// 上下文结构由消息写入与恢复边界保证，此处不复查。
func encodeContext(ctx appcontext.ModelContext, config modelapi.Config) (anthropicContext, error) {
	result := anthropicContext{System: ctx.SystemPrompt}
	ids := providerapi.BuildToolCallIDMap(ctx)
	for index, entry := range ctx.Entries {
		messages, err := anthropicContextEntry(entry, config, ids)
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
			var block blockHeader
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

func anthropicContextEntry(entry *sessionentry.ContextEntry, config modelapi.Config, ids map[string]string) ([]anthropicMessage, error) {
	var messages []anthropicMessage
	for index, block := range entry.Content {
		messageRole := string(entry.Role)
		var content any
		var raw json.RawMessage
		switch block := block.(type) {
		case sessionentry.ThinkingBlock:
			continue
		case sessionentry.TextBlock:
			if entry.Role == role.Tool {
				return nil, errors.New("anthropic tool role requires tool results")
			}
			content = requestTextBlock{Type: "text", Text: block.Text}
		case sessionentry.ToolCallBlock:
			if entry.Role != role.Assistant {
				return nil, errors.New("anthropic tool_use requires assistant role")
			}
			content = requestToolUseBlock{
				Type:  "tool_use",
				ID:    ids[block.CallID],
				Name:  string(block.Name),
				Input: json.RawMessage(block.Arguments),
			}
		case sessionentry.ToolResultBlock:
			messageRole = "user"
			content = requestToolResultBlock{
				Type:      "tool_result",
				ToolUseID: ids[block.CallID],
				Content:   block.Text,
				IsError:   block.IsError,
			}
		case sessionentry.ReasoningStateBlock:
			state := block.State
			if !config.CanReplayReasoningState(state) {
				continue
			}
			raw = bytes.Clone(state.Payload)
		default:
			return nil, fmt.Errorf("context block %d has unsupported type %T", index, block)
		}
		if messageRole != "user" && messageRole != "assistant" {
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
		if last >= 0 && messages[last].Role == messageRole {
			messages[last].Content = append(messages[last].Content, raw)
		} else {
			messages = append(messages, anthropicMessage{Role: messageRole, Content: []json.RawMessage{raw}})
		}
	}
	return messages, nil
}

func sensitiveStateValues(ctx appcontext.ModelContext) []string {
	var values []string
	for _, entry := range ctx.Entries {
		for _, block := range entry.Content {
			state, ok := block.(sessionentry.ReasoningStateBlock)
			if !ok {
				continue
			}
			var content struct {
				Signature string `json:"signature"`
				Data      string `json:"data"`
			}
			if json.Unmarshal(state.State.Payload, &content) == nil {
				values = append(values, content.Signature, content.Data)
			}
		}
	}
	return values
}
