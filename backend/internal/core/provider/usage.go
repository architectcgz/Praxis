package provider

import (
	"errors"
	"math"
	"strings"

	"praxis/internal/identity"
)

// Usage 记录一次模型请求的累计 token 用量；InputTokens 包含缓存读取和写入部分。
// 可选计数为 nil 表示 Provider 未提供该值，不能当作零用量。
type Usage struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

// Valid 校验 Provider 返回的计数；无效计数不应影响模型响应，只是不参与统计。
func (u Usage) Valid() bool {
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

// UsageRecord 按 Turn 保存请求用量，同一请求的累计更新不重复计费。
type UsageRecord struct {
	SessionID   string          `json:"sessionId"`
	AgentID     string          `json:"agentId"`
	ExecutionID string          `json:"executionId"`
	TurnID      identity.TurnID `json:"turnId"`
	Usage       Usage           `json:"usage"`
}

// Key 返回请求的稳定身份，供持久化和实时更新去重。
func (r UsageRecord) Key() string {
	return "usage:" + r.TurnID.String()
}

// Validate 检查对象自身的完整性，不修改或修复输入。
func (r UsageRecord) Validate() error {
	for _, id := range []string{
		r.SessionID,
		r.AgentID,
		r.ExecutionID,
		r.TurnID.String(),
	} {
		if id == "" || id != strings.TrimSpace(id) || strings.ContainsAny(id, "\x00\r\n") {
			return errors.New("token usage identity must be a non-empty canonical string")
		}
	}
	if !r.Usage.Valid() {
		return errors.New("invalid token usage counts")
	}
	return nil
}

// Includes 判断累计用量是否包含已保存的计数；已知计数不能丢失或倒退。
func (u Usage) Includes(previous Usage) bool {
	if u.InputTokens < previous.InputTokens {
		return false
	}
	for _, pair := range [][2]*int64{
		{previous.OutputTokens, u.OutputTokens},
		{previous.CacheReadInputTokens, u.CacheReadInputTokens},
		{previous.CacheCreationInputTokens, u.CacheCreationInputTokens},
	} {
		if pair[0] != nil && (pair[1] == nil || *pair[1] < *pair[0]) {
			return false
		}
	}
	return true
}
