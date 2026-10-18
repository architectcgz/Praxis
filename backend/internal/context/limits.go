package context

// Limits 是模型窗口及输出预留，不包含业务资源预算。
// 两个字段的正数不变量由配置输入边界保证：request 保存模型配置与 JSONL 恢复 Task 时校验，
// 内部只读取已冻结的值，不再复查。
type Limits struct {
	ContextWindow   int
	MaxOutputTokens int
}
