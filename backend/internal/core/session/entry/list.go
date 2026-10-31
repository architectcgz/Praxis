package entry

// List 的消息与序号边界来自同一次会话状态读取。
type List struct {
	SequenceBoundary uint64
	Entries          []ContextEntry
}
