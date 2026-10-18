// Package contracts 定义用量查询的公开快照。
package contracts

// ModelUsage 保留未知 token 计数与已知零值的区别。
type ModelUsage struct {
	InputTokens              int64
	OutputTokens             *int64
	CacheReadInputTokens     *int64
	CacheCreationInputTokens *int64
}

// ModelUsageRecord 是一次请求的持久化用量数据。
type ModelUsageRecord struct {
	SessionID string
	AgentID   string
	TaskID    string
	TurnID    string
	Usage     ModelUsage
}

// SessionUsageSummary 保留同一快照的记录与可选缓存比例。
type SessionUsageSummary struct {
	Records              []ModelUsageRecord
	InputTokens          int64
	CacheReadInputTokens int64
	CacheReadRatio       *float64
	CacheReadComplete    bool
}
