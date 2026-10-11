package openaichat

import appcontext "praxis/internal/core/context"

// encodeContext 只映射上下文协议，不读取模型配置。
func encodeContext(ctx appcontext.ModelContext) ([]chatMessage, error) {
	messages := make([]chatMessage, 0, len(ctx.Entries)+1)
	if ctx.SystemPrompt != "" {
		messages = append(messages, chatMessage{
			Role:    "system",
			Content: ctx.SystemPrompt,
		})
	}
	for _, entry := range ctx.Entries {
		messages = append(messages, chatMessageFromContext(entry))
	}
	return messages, nil
}

func chatMessageFromContext(entry appcontext.ContextEntry) chatMessage {
	result := chatMessage{Role: string(entry.Role)}
	for _, block := range entry.Content {
		switch block.Kind {
		case appcontext.ContextBlockText:
			result.Content += block.Text
		case appcontext.ContextBlockToolCall:
			result.ToolCalls = append(result.ToolCalls, chatToolCall{
				ID:   block.CallID,
				Type: "function",
				Function: chatFunctionCall{
					Name:      block.Name,
					Arguments: string(block.Input),
				},
			})
		case appcontext.ContextBlockToolResult:
			result.Role = "tool"
			result.ToolCallID = block.CallID
			result.Content += block.Text
		}
	}
	return result
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}
