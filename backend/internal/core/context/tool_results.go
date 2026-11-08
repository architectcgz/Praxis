package context

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

// pairToolResults 把回执放到对应调用之后，恢复补写不打断工具交互的连续性。
// 调用方负责选择模型可见内容；这里只调整顺序，使用 Run 和 call ID 避免跨任务误配。
func pairToolResults(entries []sessionentry.ContextEntry) []sessionentry.ContextEntry {
	type callKey struct{ runID, callID string }
	results := make(map[callKey]sessionentry.ContextEntry)
	calls := make(map[callKey]bool)
	for _, entry := range entries {
		for _, block := range entry.Content {
			switch value := block.(type) {
			case sessionentry.ToolCallBlock:
				calls[callKey{entry.RunID, value.CallID}] = true
			case sessionentry.ToolResultBlock:
				result := entry
				result.Content = []sessionentry.ContentBlock{value}
				results[callKey{entry.RunID, value.CallID}] = result
			}
		}
	}
	selected := make([]sessionentry.ContextEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Role == role.Tool {
			content := make([]sessionentry.ContentBlock, 0, len(entry.Content))
			for _, block := range entry.Content {
				result, ok := block.(sessionentry.ToolResultBlock)
				if !ok || !calls[callKey{entry.RunID, result.CallID}] {
					content = append(content, block)
				}
			}
			if len(content) == 0 {
				continue
			}
			entry.Content = content
		}
		selected = append(selected, entry)
		for _, block := range entry.Content {
			call, ok := block.(sessionentry.ToolCallBlock)
			if !ok {
				continue
			}
			if result, ok := results[callKey{entry.RunID, call.CallID}]; ok {
				selected = append(selected, result)
			}
		}
	}
	return selected
}
