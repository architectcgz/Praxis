package session

import (
	appcontext "praxis/internal/core/context"
	sessionentry "praxis/internal/core/session/entry"
	"slices"
)

// History 是一个 Session 消息流的内存状态；执行流由 Loop 单独提供。
// 生命周期由应用管理，Entry 追加后不修改，读取不访问持久化介质。
type History struct {
	entries               []sessionentry.ContextEntry
	byID                  map[string]int
	SessionContextEntries *appcontext.ModelContext
}

func (h *History) Append(value sessionentry.ContextEntry) {
	if h.byID == nil {
		h.byID = make(map[string]int)
	}
	if _, exists := h.byID[value.ID]; exists {
		return
	}
	h.byID[value.ID] = len(h.entries)
	h.entries = append(h.entries, value)
}

func (h *History) List() []sessionentry.ContextEntry { return slices.Clone(h.entries) }

func (h *History) Get(id string) (sessionentry.ContextEntry, bool) {
	index, ok := h.byID[id]
	if !ok {
		return sessionentry.ContextEntry{}, false
	}
	return h.entries[index], true
}

func (h *History) SetContext(value appcontext.ModelContext) *appcontext.ModelContext {
	h.SessionContextEntries = &value
	return h.SessionContextEntries
}

// ContextHistory 应用历史中的摘要替代范围，返回模型构建使用的有效历史。
// 摘要按被替代区段的位置插入，原始 List 始终保留完整记录。
func (h *History) ContextHistory() []sessionentry.ContextEntry {
	values := make([]sessionentry.ContextEntry, 0, len(h.entries))
	for _, value := range h.entries {
		if value.Kind != sessionentry.ContextEntrySummary {
			values = append(values, value)
			continue
		}
		replaced := make(map[string]bool, len(value.Replaces))
		for _, id := range value.Replaces {
			replaced[id] = true
		}
		next := make([]sessionentry.ContextEntry, 0, len(values)+1)
		inserted := false
		for _, prior := range values {
			if !replaced[prior.ID] {
				next = append(next, prior)
				continue
			}
			if !inserted {
				next = append(next, value)
				inserted = true
			}
		}
		if !inserted {
			next = append(next, value)
		}
		values = next
	}
	return values
}
