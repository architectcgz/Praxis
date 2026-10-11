package anthropicmessages

import (
	"encoding/json"
	"fmt"

	appcontext "praxis/internal/core/context"
)

type anthropicContext struct {
	System   string
	Messages []anthropicMessage
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []map[string]any `json:"content"`
}

// encodeContext 将 System 与消息分别映射；未支持的内容块返回错误，不读取模型配置。
func encodeContext(ctx appcontext.ModelContext) (anthropicContext, error) {
	result := anthropicContext{
		System:   ctx.SystemPrompt,
		Messages: make([]anthropicMessage, 0, len(ctx.Entries)),
	}
	for index, entry := range ctx.Entries {
		message, err := anthropicContextEntry(entry)
		if err != nil {
			return anthropicContext{}, fmt.Errorf("anthropic context entry %d: %w", index, err)
		}
		result.Messages = append(result.Messages, message)
	}
	return result, nil
}

func anthropicContextEntry(entry appcontext.ContextEntry) (anthropicMessage, error) {
	content := make([]map[string]any, 0, len(entry.Content))
	role := string(entry.Role)
	for index, block := range entry.Content {
		switch block.Kind {
		case appcontext.ContextBlockText:
			content = append(content, map[string]any{"type": "text", "text": block.Text})
		case appcontext.ContextBlockToolCall:
			content = append(content, map[string]any{
				"type":  "tool_use",
				"id":    block.CallID,
				"name":  block.Name,
				"input": json.RawMessage(block.Input),
			})
		case appcontext.ContextBlockToolResult:
			role = "user"
			content = append(content, map[string]any{
				"type":        "tool_result",
				"tool_use_id": block.CallID,
				"content":     block.Text,
				"is_error":    block.IsError,
			})
		default:
			return anthropicMessage{}, fmt.Errorf("context block %d has unsupported kind %q", index, block.Kind)
		}
	}
	return anthropicMessage{Role: role, Content: content}, nil
}
