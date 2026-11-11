package provider

import "praxis/internal/identity"

// Usage 记录一次模型请求的累计 token 用量；InputTokens 包含缓存读取和写入部分。
// 可选计数为 nil 表示 Provider 未提供该值，不能当作零用量。
type Usage struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

// UsageRecord 是按请求查询的已知用量投影，执行累计值由 Run 保存。
type UsageRecord struct {
	SessionID string                  `json:"sessionId"`
	AgentID   string                  `json:"agentId"`
	RunID     string                  `json:"runId"`
	RequestID identity.ModelRequestID `json:"requestId"`
	TurnID    identity.TurnID         `json:"turnId,omitempty"`
	Usage     Usage                   `json:"usage"`
}
