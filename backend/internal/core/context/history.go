package context

import sessionmodel "praxis/internal/core/session"

// historyEntries 把回执关联到对应调用后，恢复补写和执行期间入队的用户消息不打断工具交互。
// 只调整模型投影，不改写日志顺序；使用 Task 和 call ID 避免跨任务误配同名调用。
func historyEntries(messages []sessionmodel.MessageData) []ContextEntry {
	type callKey struct{ taskID, callID string }
	results := make(map[callKey]ContextBlock)
	calls := make(map[callKey]bool)
	for _, message := range messages {
		if entry, ok := EntryFromMessageData(message); ok {
			for _, block := range entry.Content {
				if block.Kind == ContextBlockToolCall {
					calls[callKey{message.TaskID, block.CallID}] = true
				}
				if block.Kind == ContextBlockToolResult {
					results[callKey{message.TaskID, block.CallID}] = block
				}
			}
		}
	}
	entries := make([]ContextEntry, 0, len(messages))
	for _, message := range messages {
		entry, ok := EntryFromMessageData(message)
		if !ok {
			continue
		}
		if message.Role == sessionmodel.RoleTool {
			content := make([]ContextBlock, 0, len(entry.Content))
			for _, block := range entry.Content {
				if block.Kind != ContextBlockToolResult || !calls[callKey{message.TaskID, block.CallID}] {
					content = append(content, block)
				}
			}
			if len(content) == 0 {
				continue
			}
			entry.Content = content
		}
		entries = append(entries, entry)
		for _, block := range entry.Content {
			if block.Kind != ContextBlockToolCall {
				continue
			}
			if result, ok := results[callKey{message.TaskID, block.CallID}]; ok {
				entries = append(entries, ContextEntry{
					Kind:    ContextEntryToolResult,
					Role:    ContextRoleTool,
					Content: []ContextBlock{result},
				})
			}
		}
	}
	return entries
}
