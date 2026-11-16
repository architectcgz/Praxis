package context

import sessionentry "praxis/internal/core/entry"

// ModelContextFromEntries 使用已经按事实语义选中的原文，摘要置前；摘要输入只包含最近摘要及其后的新增记录。
func ModelContextFromEntries(systemPrompt string, entries []*sessionentry.ContextEntry) ModelContext {
	selected := entries
	summaryInput := entries
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].IsSummary() {
			selected = make([]*sessionentry.ContextEntry, 0, len(entries))
			selected = append(selected, entries[i])
			selected = append(selected, entries[:i]...)
			selected = append(selected, entries[i+1:]...)
			summaryInput = entries[i:]
			break
		}
	}
	return ModelContext{SystemPrompt: systemPrompt, Entries: ModelEntries(selected), SummaryInput: ModelEntries(summaryInput)}
}
