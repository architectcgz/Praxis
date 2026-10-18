package model

import (
	"math"
	"praxis/internal/identity"
)

// ModelUsage 记录一次模型请求的累计 token 用量；InputTokens 包含缓存读取和写入部分。
// 可选计数为 nil 表示 Provider 未提供该值，不能当作零用量。
type ModelUsage struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

// Valid 校验 Provider 返回的计数；无效计数不应影响模型响应，只是不参与统计。
func (u ModelUsage) Valid() bool {
	if u.InputTokens < 0 {
		return false
	}
	if u.OutputTokens != nil && (*u.OutputTokens < 0 || *u.OutputTokens > math.MaxInt64-u.InputTokens) {
		return false
	}
	var read, creation int64
	if u.CacheReadInputTokens != nil {
		read = *u.CacheReadInputTokens
	}
	if u.CacheCreationInputTokens != nil {
		creation = *u.CacheCreationInputTokens
	}
	return read >= 0 && creation >= 0 && creation <= u.InputTokens && read <= u.InputTokens-creation
}

// ModelUsageRecord 按 Turn 保存请求用量，同一请求的累计更新不重复计费。
type ModelUsageRecord struct {
	SessionID   string          `json:"sessionId"`
	AgentID     string          `json:"agentId"`
	ExecutionID string          `json:"executionId"`
	TurnID      identity.TurnID `json:"turnId"`
	Usage       ModelUsage      `json:"usage"`
}

// Key 返回请求的稳定身份，供持久化和实时更新去重。
func (r ModelUsageRecord) Key() string {
	return "usage:" + r.TurnID.String()
}
