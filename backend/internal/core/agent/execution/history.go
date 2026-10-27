package execution

import sessionentry "praxis/internal/core/session/entry"

// History 保存本次执行的条目证据；子执行同时以它作为私有历史。
// 主会话的跨执行历史由 Session 管理，正文追加后不可变。
type History struct {
	entries []sessionentry.ContextEntry
	byID    map[string]int
}

// Append 追加条目；调用方负责保证 ID 唯一。
func (h *History) Append(value sessionentry.ContextEntry) {
	if h.byID == nil {
		h.byID = make(map[string]int)
	}
	h.byID[value.ID] = len(h.entries)
	h.entries = append(h.entries, value.Clone())
}

func (h *History) List() []sessionentry.ContextEntry {
	values := make([]sessionentry.ContextEntry, len(h.entries))
	for index, entry := range h.entries {
		values[index] = entry.Clone()
	}
	return values
}

func (h *History) Get(id string) (sessionentry.ContextEntry, bool) {
	index, ok := h.byID[id]
	if !ok {
		return sessionentry.ContextEntry{}, false
	}
	return h.entries[index].Clone(), true
}
