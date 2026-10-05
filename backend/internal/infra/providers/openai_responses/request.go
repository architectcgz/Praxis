package openairesponses

import (
	"strings"

	appcontext "praxis/internal/core/context"
)

func responseItems(entry appcontext.ContextEntry) []responseItem {
	items := make([]responseItem, 0, len(entry.Content))
	var text strings.Builder
	flushText := func() {
		if text.Len() == 0 {
			return
		}
		items = append(items, responseItem{
			Type: "message",
			Role: string(entry.Role),
			Content: []responseContentPart{{
				Type: responseTextPartType(entry.Role),
				Text: text.String(),
			}},
		})
		text.Reset()
	}
	for _, block := range entry.Content {
		switch block.Kind {
		case appcontext.ContextBlockText:
			text.WriteString(block.Text)
		case appcontext.ContextBlockToolCall:
			flushText()
			items = append(items, responseItem{
				Type:      "function_call",
				CallID:    block.CallID,
				Name:      block.Name,
				Arguments: string(block.Input),
			})
		case appcontext.ContextBlockToolResult:
			flushText()
			items = append(items, responseItem{
				Type:   "function_call_output",
				CallID: block.CallID,
				Output: block.Text,
			})
		}
	}
	flushText()
	return items
}

func responseTextPartType(role appcontext.ContextRole) string {
	if role == appcontext.ContextRoleAssistant {
		return "output_text"
	}
	return "input_text"
}
