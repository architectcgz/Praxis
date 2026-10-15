package context

// Content 是调用方已选择并排序的通用模型内容，不携带业务身份或消息日志。
type Content struct {
	SystemPrompt string
	Entries      []ContextEntry
}
