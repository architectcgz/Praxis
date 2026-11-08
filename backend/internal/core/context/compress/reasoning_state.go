package compress

import (
	sessionentry "praxis/internal/core/session/entry"
)

// summaryEntries 不把签名、加密载荷或展示性思考正文发送给摘要模型。
// 只构建序列化所需的块列表，共享载荷始终只读。
func summaryEntries(entries []sessionentry.ContextEntry) []sessionentry.ContextEntry {
	var selected []sessionentry.ContextEntry
	for _, entry := range entries {
		copy := entry
		copy.Content = nil
		for _, block := range entry.Content {
			switch block.(type) {
			case sessionentry.ReasoningStateBlock, sessionentry.ThinkingBlock:
			default:
				copy.Content = append(copy.Content, block)
			}
		}
		if len(copy.Content) > 0 {
			selected = append(selected, copy)
		}
	}
	return selected
}
