package openaichat

import appcontext "praxis/internal/core/context"

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
