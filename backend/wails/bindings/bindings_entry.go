package bindings

import (
	"bytes"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/wails/dto"
)

// publicEntries 为查询和事件推送提供相同的展示投影，空集合统一编码为数组。
// 内容块按白名单映射，内部推理续传状态和未知块不会穿过协议边界。
func publicEntries(values []sessionentry.ContextEntry) []dto.Entry {
	result := make([]dto.Entry, 0, len(values))
	for _, value := range values {
		content := make([]dto.EntryBlock, 0, len(value.Content))
		for _, block := range value.Content {
			switch block.Kind {
			case sessionentry.BlockText, sessionentry.BlockThinking:
				content = append(content, dto.EntryBlock{
					Kind: string(block.Kind),
					Text: block.Text,
				})
			case sessionentry.BlockToolCall:
				content = append(content, dto.EntryBlock{
					Kind:   string(block.Kind),
					CallID: block.CallID,
					Name:   block.Name,
					Input:  bytes.Clone(block.Input),
				})
			case sessionentry.BlockToolResult:
				content = append(content, dto.EntryBlock{
					Kind:    string(block.Kind),
					Text:    block.Text,
					CallID:  block.CallID,
					Name:    block.Name,
					IsError: block.IsError,
				})
			}
		}
		kind := dto.EntryMessage
		if value.IsSummary() {
			kind = dto.EntrySummary
		}
		result = append(result, dto.Entry{
			ID:          value.ID,
			ExecutionID: value.ExecutionID,
			CreatedAt:   value.CreatedAt,
			Role:        string(value.Role),
			Kind:        kind,
			Content:     content,
		})
	}
	return result
}
