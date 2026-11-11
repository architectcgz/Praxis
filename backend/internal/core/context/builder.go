package context

import sessionentry "praxis/internal/core/session/entry"

// ModelContextFromEntries 选择最近摘要、保留原文和后续历史，只建立只读 Entry 引用列表。
func ModelContextFromEntries(systemPrompt string, entries []*sessionentry.ContextEntry) ModelContext {
	return ModelContext{
		SystemPrompt: systemPrompt,
		Entries:      ModelEntries(entriesFromSummary(entries)),
	}
}

// entriesFromSummary 恢复摘要之前仍需保留的原文；摘要追加到历史末尾，不改变原文位置。
func entriesFromSummary(entries []*sessionentry.ContextEntry) []*sessionentry.ContextEntry {
	for index := len(entries) - 1; index >= 0; index-- {
		summary := entries[index]
		if !summary.IsSummary() {
			continue
		}
		ids := make(map[string]bool, len(summary.Compression.RetainedEntryIDs))
		for _, id := range summary.Compression.RetainedEntryIDs {
			ids[id] = true
		}
		selected := make([]*sessionentry.ContextEntry, 0, len(ids)+len(entries)-index)
		selected = append(selected, summary)
		for _, entry := range entries[:index] {
			if ids[entry.ID] {
				selected = append(selected, entry)
			}
		}
		return append(selected, entries[index+1:]...)
	}
	return entries
}
