package dto

// ModelUsage 保留 Provider 未上报计数与已知零值的区别。
type ModelUsage struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

// ModelUsageRecord 是一次模型请求的持久化用量。
type ModelUsageRecord struct {
	SessionID   string     `json:"sessionId"`
	AgentID     string     `json:"agentId"`
	ExecutionID string     `json:"executionId"`
	TurnID      string     `json:"turnId"`
	Usage       ModelUsage `json:"usage"`
}

// SessionUsageSummary 返回同一快照的用量明细和加权缓存率；未知比例编码为 null。
type SessionUsageSummary struct {
	Records              []ModelUsageRecord `json:"records"`
	InputTokens          int64              `json:"inputTokens"`
	CacheReadInputTokens int64              `json:"cacheReadInputTokens"`
	CacheReadRatio       *float64           `json:"cacheReadRatio"`
	CacheReadComplete    bool               `json:"cacheReadComplete"`
}
