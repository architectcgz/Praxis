package openairesponses

import (
	"strings"

	appcontext "praxis/internal/core/context"
)

// encodeContext 只映射上下文协议，不读取模型配置。
func encodeContext(ctx appcontext.ModelContext) ([]responseItem, error) {
	input := make([]responseItem, 0, len(ctx.Entries)+1)
	if ctx.SystemPrompt != "" {
		input = append(input, responseItem{
			Type: "message",
			Role: "system",
			Content: []responseContentPart{{
				Type: "input_text",
				Text: ctx.SystemPrompt,
			}},
		})
	}
	for _, entry := range ctx.Entries {
		input = append(input, responseItems(entry)...)
	}
	return input, nil
}

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

type responseItem struct {
	Type      string                `json:"type"`
	Role      string                `json:"role,omitempty"`
	Content   []responseContentPart `json:"content,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	Output    string                `json:"output,omitempty"`
}

type responseContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}
