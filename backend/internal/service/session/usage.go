package session

import (
	"context"
	"errors"
	"math"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
)

// SessionUsageSummary 是会话全部 Agent 已保存请求的用量快照；百分比不持久化。
// CacheReadRatio 使用 0 到 1，零输入或存在未知缓存计数时为 nil。
// CacheReadComplete 只表示 Records 内的缓存读取计数完整，不推算未上报的请求。
type SessionUsageSummary struct {
	Records              []providerapi.UsageRecord
	InputTokens          int64
	CacheReadInputTokens int64
	CacheReadRatio       *float64
	CacheReadComplete    bool
}

// ListSessionUsage 查询完整请求用量，不受消息分页影响。
func (s *Service) ListSessionUsage(ctx context.Context, sessionID identity.SessionID) ([]providerapi.UsageRecord, error) {
	return s.usageRecords.ListSession(ctx, sessionID.String())
}

// GetSessionUsageSummary 查询会话全部最新用量记录，以 token 加权计算缓存率。
// 读取失败、记录归属不一致或累计值溢出时返回错误，未知计数不补零。
func (s *Service) GetSessionUsageSummary(ctx context.Context, sessionID identity.SessionID) (SessionUsageSummary, error) {
	if err := ctx.Err(); err != nil {
		return SessionUsageSummary{}, err
	}
	records, err := s.usageRecords.ListSession(ctx, sessionID.String())
	if err != nil {
		return SessionUsageSummary{}, err
	}
	if records == nil {
		records = []providerapi.UsageRecord{}
	}
	summary := SessionUsageSummary{
		Records:           records,
		CacheReadComplete: true,
	}
	for _, record := range records {
		if record.SessionID != sessionID.String() {
			return SessionUsageSummary{}, errors.New("session usage record ownership mismatch")
		}
		if record.Usage.InputTokens > math.MaxInt64-summary.InputTokens {
			return SessionUsageSummary{}, errors.New("session input token counter overflow")
		}
		summary.InputTokens += record.Usage.InputTokens
		if record.Usage.CacheReadInputTokens == nil {
			summary.CacheReadComplete = false
		} else {
			summary.CacheReadInputTokens += *record.Usage.CacheReadInputTokens
		}
	}
	if summary.CacheReadComplete && summary.InputTokens > 0 {
		ratio := float64(summary.CacheReadInputTokens) / float64(summary.InputTokens)
		summary.CacheReadRatio = &ratio
	}
	return summary, nil
}
