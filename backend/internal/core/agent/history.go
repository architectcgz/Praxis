package agent

import (
	sessionentry "praxis/internal/core/session/entry"
	"slices"
)

// History 是一个 Agent 的唯一消息历史；Session 对话直接使用 Main Agent 的历史。
// 历史由存储层管理，Entry 追加后不修改，不持有执行期间的可变模型上下文。
type History struct {
	entries []sessionentry.ContextEntry
	byID    map[string]int
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

// ContextHistory 应用历史中的摘要替代范围，返回模型构建使用的有效历史。
// 摘要按被替代区段的位置插入，原始 List 始终保留完整记录。
func (h *History) ContextHistory() []sessionentry.ContextEntry {
	values := make([]sessionentry.ContextEntry, 0, len(h.entries))
	for _, value := range h.entries {
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
