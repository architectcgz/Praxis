package compress

import (
	appcontext "praxis/internal/context"
)

// summaryHistory 不把签名、加密载荷或展示性思考正文发送给摘要模型。
// 只作用于可安全丢弃的连续历史区段，保留区段仍使用原始深拷贝。
func summaryHistory(entries []appcontext.ContextEntry) []appcontext.ContextEntry {
	var history []appcontext.ContextEntry
	for _, entry := range entries {
		copy := entry
		copy.Content = nil
		for _, block := range entry.Content {
			if block.Kind != appcontext.ContextBlockReasoningState && block.Kind != appcontext.ContextBlockThinking {
				copy.Content = append(copy.Content, block.Clone())
			}
		}
		if len(copy.Content) > 0 {
			history = append(history, copy)
		}
	}
	return history
}
