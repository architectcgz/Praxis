package execution

import (
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/core/session/entry/role"
	"praxis/internal/core/tool"
	"praxis/internal/errorcode"
)

// History 保存本次执行的条目证据；子执行同时以它作为私有历史。
// 主会话的跨执行历史由 Session 管理，已提交工具回执仅允许更新正文。
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

// UpdateToolResult 保留原条目位置和元数据，只接纳工具回执正文修改。
func (h *History) UpdateToolResult(id, text string) (sessionentry.ContextEntry, error) {
	index, ok := h.byID[id]
	if !ok {
		return sessionentry.ContextEntry{}, errorcode.ErrNotFound
	}
	entry := &h.entries[index]
	if entry.Role != role.Tool || len(entry.Content) != 1 || entry.Content[0].Kind != sessionentry.BlockToolResult {
		return sessionentry.ContextEntry{}, errorcode.ErrRequestConflict
	}
	if len(text) > tool.MaxInlineResultBytes {
		return sessionentry.ContextEntry{}, errorcode.ErrInvalidRequest
	}
	entry.Content[0].Text = text
	return entry.Clone(), nil
}
