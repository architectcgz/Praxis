package execution

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/errorcode"
	"reflect"
)

// History 只属于本次 Execution；正文追加后不可变，调用方取得独立副本。
type History struct {
	entries []sessionentry.ContextEntry
	byID    map[string]int
}

func (h *History) Append(value sessionentry.ContextEntry) (bool, error) {
	if h.byID == nil {
		h.byID = make(map[string]int)
	}
	if index, exists := h.byID[value.ID]; exists {
		prior := h.entries[index]
		value.Sequence, value.CreatedAt = prior.Sequence, prior.CreatedAt
		if !reflect.DeepEqual(prior, value) {
			return false, errorcode.ErrRequestConflict
		}
		return false, nil
	}
	h.byID[value.ID] = len(h.entries)
	h.entries = append(h.entries, value.Clone())
	return true, nil
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

// ContextHistory 应用历史中的摘要替代范围，返回模型构建使用的有效历史。
// 摘要按被替代区段的位置插入，原始 List 始终保留完整记录。
func (h *History) ContextHistory() []sessionentry.ContextEntry {
	values := make([]sessionentry.ContextEntry, 0, len(h.entries))
	for _, value := range h.List() {
		if !value.IsSummary() {
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
