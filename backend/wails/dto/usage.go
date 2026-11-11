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
	SessionID string     `json:"sessionId"`
	AgentID   string     `json:"agentId"`
	RunID     string     `json:"runId"`
	RequestID string     `json:"requestId"`
	TurnID    string     `json:"turnId,omitempty"`
	Usage     ModelUsage `json:"usage"`
}

// SessionUsageSummary 只返回 Run 累计与已知性，不附带 Turn 明细；未知比例编码为 null。
type SessionUsageSummary struct {
	TotalRequests            uint64   `json:"totalRequests"`
	InputTokens              int64    `json:"inputTokens"`
	OutputTokens             int64    `json:"outputTokens"`
	CacheReadInputTokens     int64    `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64    `json:"cacheCreationInputTokens"`
	ReportedRequests         uint64   `json:"reportedRequests"`
	OutputReports            uint64   `json:"outputReports"`
	CacheReadReports         uint64   `json:"cacheReadReports"`
	CacheCreationReports     uint64   `json:"cacheCreationReports"`
	CacheReadRatio           *float64 `json:"cacheReadRatio"`
	CacheReadComplete        bool     `json:"cacheReadComplete"`
}
