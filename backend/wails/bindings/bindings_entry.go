package bindings

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/wails/dto"
)

// publicEntries 为查询和事件推送提供相同的展示投影，空集合统一编码为数组。
// 内容块按白名单映射，内部推理续传状态和未知块不会穿过协议边界。
func publicEntries(values []sessionentry.ContextEntry) []dto.Entry {
	result := make([]dto.Entry, 0, len(values))
	for _, value := range values {
		projected := dto.Entry{}
		if value.Response != nil {
			projected.RequestID = value.Response.RequestID.String()
			projected.Error = value.Response.Error
			projected.Timing = value.Response.Timing
		} else if value.Compression != nil {
			projected.Timing = value.Compression.Timing
		}
		content := make([]dto.EntryBlock, 0, len(value.Content))
		for _, block := range value.Content {
			switch block := block.(type) {
			case sessionentry.TextBlock:
				content = append(content, dto.EntryBlock{
					Kind: "text",
					Text: block.Text,
				})
			case sessionentry.ThinkingBlock:
				content = append(content, dto.EntryBlock{
					Kind: "thinking",
					Text: block.Text,
				})
			case sessionentry.ToolCallBlock:
				content = append(content, dto.EntryBlock{
					Kind:   "tool_call",
					CallID: block.CallID,
					Name:   string(block.Name),
					Input:  block.Arguments,
				})
			case sessionentry.ToolResultBlock:
				projected.Timing = block.Timing
				content = append(content, dto.EntryBlock{
					Kind:    "tool_result",
					Text:    block.Text,
					CallID:  block.CallID,
					Name:    string(block.Name),
					IsError: block.IsError,
				})
			}
		}
		kind := dto.EntryMessage
		if value.IsSummary() {
			kind = dto.EntrySummary
		} else if value.IsRun() {
			kind = dto.EntryRun
		}
		result = append(result, dto.Entry{
			ID:        value.ID,
			RunID:     value.RunID,
			TurnID:    value.TurnID,
			CreatedAt: value.CreatedAt,
			Role:      string(value.Role),
			Kind:      kind,
			Content:   content,
			RequestID: projected.RequestID,
			Error:     projected.Error,
			Timing:    projected.Timing,
		})
	}
	return result
}
