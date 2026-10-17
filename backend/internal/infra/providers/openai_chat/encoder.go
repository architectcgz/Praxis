package openaichat

import (
	"fmt"

	appcontext "praxis/internal/core/context"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/providers/streaming"
)

// encodeContext 编码官方文本与工具消息；不支持续传的推理状态只在本次请求中忽略。
func encodeContext(ctx appcontext.ModelContext) ([]chatMessage, error) {
	if err := ctx.Validate(); err != nil {
		return nil, err
	}
	messages := make([]chatMessage, 0, len(ctx.Entries)+1)
	if ctx.SystemPrompt != "" {
		messages = append(messages, chatMessage{Role: "system", Content: &ctx.SystemPrompt})
	}
	ids := providers.BuildToolCallIDMap(ctx)
	for index, entry := range ctx.Entries {
		encoded, err := chatMessagesFromContext(entry, ids)
		if err != nil {
			return nil, fmt.Errorf("openai chat context entry %d: %w", index, err)
		}
		messages = append(messages, encoded...)
	}
	return messages, nil
}

func chatMessagesFromContext(entry appcontext.ContextEntry, ids map[string]string) ([]chatMessage, error) {
	var messages []chatMessage
	result := chatMessage{Role: string(entry.Role)}
	flush := func() {
		if result.Content != nil || len(result.ToolCalls) > 0 {
			messages = append(messages, result)
			result = chatMessage{Role: string(entry.Role)}
		}
	}
	for index, block := range entry.Content {
		switch block.Kind {
		case appcontext.ContextBlockText:
			if entry.Role == appcontext.ContextRoleTool {
				return nil, fmt.Errorf("tool role requires tool results")
			}
			if result.Content == nil {
				text := block.Text
				result.Content = &text
			} else {
				*result.Content += block.Text
			}
		case appcontext.ContextBlockToolCall:
			if entry.Role != appcontext.ContextRoleAssistant {
				return nil, fmt.Errorf("tool call requires assistant role")
			}
			if err := streaming.ValidateToolCall(&streaming.ToolAccumulator{ID: block.CallID, Name: block.Name, Arguments: string(block.Input)}); err != nil {
				return nil, err
			}
			result.ToolCalls = append(result.ToolCalls, chatToolCall{
				ID:       ids[block.CallID],
				Type:     "function",
				Function: chatFunctionCall{Name: block.Name, Arguments: string(block.Input)},
			})
		case appcontext.ContextBlockToolResult:
			flush()
			text := block.Text
			messages = append(messages, chatMessage{Role: "tool", ToolCallID: ids[block.CallID], Content: &text})
		case appcontext.ContextBlockReasoningState:
			continue
		default:
			return nil, fmt.Errorf("context block %d has unsupported kind %q", index, block.Kind)
		}
	}
	flush()
	return messages, nil
}
