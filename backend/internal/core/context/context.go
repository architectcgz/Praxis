package context

import (
	sessionentry "praxis/internal/core/session/entry"
	"slices"
)

// Context 持有持续更新的上下文条目，不保存执行提示词。
// Session 或子执行负责其生命周期和并发访问；完整历史由各自的历史存储保留。
type Context struct {
	entries []sessionentry.ContextEntry
}

// NewContext 从保存的上下文及后续条目初始化，不重放摘要替代关系。
func NewContext(entries []sessionentry.ContextEntry) *Context {
	value := &Context{}
	value.Replace(pairToolResults(entries))
	return value
}

// Entries 返回当前有效条目的独立快照，不重新扫描原始历史。
func (c *Context) Entries() []sessionentry.ContextEntry {
	entries := make([]sessionentry.ContextEntry, len(c.entries))
	for index, entry := range c.entries {
		entries[index] = entry.Clone()
	}
	return entries
}

// Append 只接收历史已接纳的条目；同一身份不会再次进入有效上下文。
// 摘要只随完整压缩结果进入 Replace，普通条目追加后在当前上下文内维护工具配对。
func (c *Context) Append(entry sessionentry.ContextEntry) {
	if entry.IsSummary() || slices.ContainsFunc(c.entries, func(prior sessionentry.ContextEntry) bool { return prior.ID == entry.ID }) {
		return
	}
	value := entry.Clone()
	c.entries = pairToolResults(append(c.entries, value))
}

// Replace 在压缩结果已确认后直接替换有效条目，释放被压缩原文和舍弃结果的引用。
func (c *Context) Replace(entries []sessionentry.ContextEntry) {
	values := make([]sessionentry.ContextEntry, len(entries))
	for index, entry := range entries {
		values[index] = entry.Clone()
	}
	c.entries = values
}
