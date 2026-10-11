package openaichat

import (
	"fmt"

	appcontext "praxis/internal/core/context"
)

// encodeContext 只映射上下文协议；未支持的内容块返回错误，不读取模型配置。
func encodeContext(ctx appcontext.ModelContext) ([]chatMessage, error) {
	messages := make([]chatMessage, 0, len(ctx.Entries)+1)
	if ctx.SystemPrompt != "" {
		messages = append(messages, chatMessage{
			Role:    "system",
			Content: ctx.SystemPrompt,
		})
	}
	for index, entry := range ctx.Entries {
		encoded, err := chatMessagesFromContext(entry)
		if err != nil {
			return nil, fmt.Errorf("openai chat context entry %d: %w", index, err)
		}
		messages = append(messages, encoded...)
	}
	return messages, nil
}

func chatMessagesFromContext(entry appcontext.ContextEntry) ([]chatMessage, error) {
	messages := make([]chatMessage, 0, 1)
	result := chatMessage{Role: string(entry.Role)}
	flushMessage := func() {
		if result.Content != "" || len(result.ToolCalls) > 0 {
			messages = append(messages, result)
			result = chatMessage{Role: string(entry.Role)}
		}
	}
	for index, block := range entry.Content {
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
			flushMessage()
			// Chat 每条 tool 消息只能引用一个调用，不能合并结果或覆盖前一个 CallID。
			messages = append(messages, chatMessage{
				Role:       "tool",
				ToolCallID: block.CallID,
				Content:    block.Text,
			})
		default:
			return nil, fmt.Errorf("context block %d has unsupported kind %q", index, block.Kind)
		}
	}
	flushMessage()
	if len(messages) == 0 {
		messages = append(messages, result)
	}
	return messages, nil
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
