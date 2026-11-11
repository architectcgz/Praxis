package context

import sessionentry "praxis/internal/core/session/entry"

// ModelEntries 保持 Session 提供的顺序，只排除不参与模型对话的执行事实。
func ModelEntries(entries []*sessionentry.ContextEntry) []*sessionentry.ContextEntry {
	selected := make([]*sessionentry.ContextEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Response != nil && entry.Response.Error != "" || entry.IsRun() {
			continue
		}
		for _, block := range entry.Content {
			if _, thinking := block.(sessionentry.ThinkingBlock); !thinking {
				selected = append(selected, entry)
				break
			}
		}
	}
	return selected
}
