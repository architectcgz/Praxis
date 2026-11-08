package context

import (
	sessionentry "praxis/internal/core/session/entry"
	"slices"
)

// Context 由一次 Run 持有独立的引用列表；已接纳的 Entry 及内部载荷始终只读。
type Context struct {
	entries []*sessionentry.ContextEntry
}

// BuildContext 从分支路径的最后一条摘要开始构建，不复制 Entry 内容。
func BuildContext(path []*sessionentry.ContextEntry) Context {
	start := 0
	for index := len(path) - 1; index >= 0; index-- {
		if path[index].IsSummary() {
			start = index
			break
		}
	}
	return Context{entries: slices.Clone(path[start:])}
}

func (c Context) Entries() []*sessionentry.ContextEntry {
	return slices.Clone(c.entries)
}

func (c *Context) Append(entry *sessionentry.ContextEntry) {
	if entry.IsSummary() {
		c.entries = []*sessionentry.ContextEntry{entry}
		return
	}
	c.entries = append(c.entries, entry)
}

// ModelEntries 选择模型可见内容并调整工具结果顺序，不修改共享参数或推理载荷。
func ModelEntries(entries []sessionentry.ContextEntry) []sessionentry.ContextEntry {
	selected := make([]sessionentry.ContextEntry, 0, len(entries))
	for _, entry := range entries {
		if entry.Error != "" {
			continue
		}
		if value, visible := modelVisibleEntry(entry); visible {
			selected = append(selected, value)
		}
	}
	return pairToolResults(selected)
}
