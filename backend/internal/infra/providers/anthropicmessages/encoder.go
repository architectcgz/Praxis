package anthropicmessages

import (
	"encoding/json"

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

// encodeContext 将 System 与消息分别映射，不读取模型配置。
func encodeContext(ctx appcontext.ModelContext) (anthropicContext, error) {
	result := anthropicContext{
		System:   ctx.SystemPrompt,
		Messages: make([]anthropicMessage, 0, len(ctx.Entries)),
	}
	for _, entry := range ctx.Entries {
		result.Messages = append(result.Messages, anthropicContextEntry(entry))
	}
	return result, nil
}

func anthropicContextEntry(entry appcontext.ContextEntry) anthropicMessage {
	content := make([]map[string]any, 0, len(entry.Content))
	role := string(entry.Role)
	for _, block := range entry.Content {
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
		}
	}
	return anthropicMessage{Role: role, Content: content}
}
