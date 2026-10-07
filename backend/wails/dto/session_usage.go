package dto

// SessionUsageSummary 返回同一快照的用量明细和加权缓存率；未知比例编码为 null。
type SessionUsageSummary struct {
	Records              []ModelUsageRecord `json:"records"`
	InputTokens          int64              `json:"inputTokens"`
	CacheReadInputTokens int64              `json:"cacheReadInputTokens"`
	CacheReadRatio       *float64           `json:"cacheReadRatio"`
	CacheReadComplete    bool               `json:"cacheReadComplete"`
}
