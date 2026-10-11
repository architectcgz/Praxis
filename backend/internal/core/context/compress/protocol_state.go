package compress

import appcontext "praxis/internal/core/context"

// protectedHistoryStart 同时保护最新用户 turn 与未结束的工具链。
// 工具结果到齐并不代表 assistant turn 完成；必须等无调用的 assistant 回答才能解除保护。
func protectedHistoryStart(value appcontext.ModelContext) int {
	start, chainStart, userIndex := len(value.Entries), -1, -1
	pending := map[string]int{}
	for index, entry := range value.Entries {
		if entry.Kind == appcontext.ContextEntryUserInput {
			start, userIndex = index, index
		}
		hasCalls := false
		for _, block := range entry.Content {
			switch block.Kind {
			case appcontext.ContextBlockToolCall, appcontext.ContextBlockMCPCall:
				hasCalls = true
				pending[block.CallID] = index
			case appcontext.ContextBlockToolResult, appcontext.ContextBlockMCPResult:
				delete(pending, block.CallID)
			}
		}
		if hasCalls && chainStart < 0 {
			chainStart = index
			if userIndex >= 0 {
				chainStart = userIndex
			}
		}
		if entry.Role == appcontext.ContextRoleAssistant && !hasCalls && len(pending) == 0 {
			chainStart = -1
		}
	}
	if chainStart >= 0 {
		start = min(start, chainStart)
	}
	for _, index := range pending {
		start = min(start, index)
	}
	return start
}

// summaryHistory 不把签名、加密载荷或展示性思考正文发送给摘要模型。
// 只作用于可安全丢弃的完整历史前缀，保留区段仍使用原始深拷贝。
func summaryHistory(entries []appcontext.ContextEntry) []appcontext.ContextEntry {
	var history []appcontext.ContextEntry
	for _, entry := range entries {
		copy := entry.Clone()
		copy.Content = nil
		for _, block := range entry.Content {
			if block.Kind != appcontext.ContextBlockOpaqueState && block.Kind != appcontext.ContextBlockThinking {
				copy.Content = append(copy.Content, block.Clone())
			}
		}
		if len(copy.Content) > 0 {
			history = append(history, copy)
		}
	}
	return history
}
