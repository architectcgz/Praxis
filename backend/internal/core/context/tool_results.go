package context

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
)

// modelVisibleEntry 选择模型可见块，保留原 Entry 身份及执行归属。
func modelVisibleEntry(value sessionentry.ContextEntry) (sessionentry.ContextEntry, bool) {
	content := make([]sessionentry.Block, 0, len(value.Content))
	for _, block := range value.Content {
		if block.Kind != sessionentry.BlockThinking {
			content = append(content, block)
		}
	}
	value.Content = content
	return value, len(content) > 0
}

// pairToolResults 把回执放到对应调用之后，恢复补写不打断工具交互的连续性。
// 只调整上下文顺序，不改写原始历史；使用 Execution 和 call ID 避免跨任务误配同名调用。
func pairToolResults(entries []sessionentry.ContextEntry) []sessionentry.ContextEntry {
	type callKey struct{ executionID, callID string }
	results := make(map[callKey]sessionentry.ContextEntry)
	calls := make(map[callKey]bool)
	for _, message := range entries {
		if entry, ok := modelVisibleEntry(message); ok {
			for _, block := range entry.Content {
				if block.Kind == sessionentry.BlockToolCall {
					calls[callKey{message.ExecutionID, block.CallID}] = true
				}
				if block.Kind == sessionentry.BlockToolResult {
					result := entry
					result.Content = []sessionentry.Block{block}
					results[callKey{message.ExecutionID, block.CallID}] = result
				}
			}
		}
	}
	selected := make([]sessionentry.ContextEntry, 0, len(entries))
	for _, message := range entries {
		entry, ok := modelVisibleEntry(message)
		if !ok {
			continue
		}
		if message.Role == role.Tool {
			content := make([]sessionentry.Block, 0, len(entry.Content))
			for _, block := range entry.Content {
				if block.Kind != sessionentry.BlockToolResult || !calls[callKey{message.ExecutionID, block.CallID}] {
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
			if block.Kind != sessionentry.BlockToolCall {
				continue
			}
			if result, ok := results[callKey{message.ExecutionID, block.CallID}]; ok {
				selected = append(selected, result)
			}
		}
	}
	return selected
}
