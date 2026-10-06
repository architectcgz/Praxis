package session

import (
	"context"
	"errors"
	"math"

	"praxis/internal/agent_runtime"
	"praxis/internal/contracts"
)

// SessionUsageSummary 是会话全部 Agent 已保存请求的用量快照；百分比不持久化。
// CacheReadRatio 使用 0 到 1，零输入或存在未知缓存计数时为 nil。
// CacheReadComplete 只表示 Records 内的缓存读取计数完整，不推算未上报的请求。
type SessionUsageSummary struct {
	Records              []agentruntime.ModelUsageRecord
	InputTokens          int64
	CacheReadInputTokens int64
	CacheReadRatio       *float64
	CacheReadComplete    bool
}

// GetSessionUsageSummary 查询会话全部最新用量记录，以 token 加权计算缓存率。
// 读取失败、记录归属不一致或累计值溢出时返回错误，未知计数不补零。
func (s *Service) GetSessionUsageSummary(ctx context.Context, sessionID contracts.SessionID) (SessionUsageSummary, error) {
	if ctx == nil {
		return SessionUsageSummary{}, errors.New("会话用量查询 context 不能为空")
	}
	if err := ctx.Err(); err != nil {
		return SessionUsageSummary{}, err
	}
	records, err := s.usageRecords(ctx, sessionID.String())
	if err != nil {
		return SessionUsageSummary{}, err
	}
	if records == nil {
		records = []agentruntime.ModelUsageRecord{}
	}
	summary := SessionUsageSummary{
		Records:           records,
		CacheReadComplete: true,
	}
	for _, record := range records {
		if record.SessionID != sessionID.String() {
			return SessionUsageSummary{}, errors.New("会话用量记录归属不一致")
		}
		if err := record.Validate(); err != nil {
			return SessionUsageSummary{}, err
		}
		if record.Usage.InputTokens > math.MaxInt64-summary.InputTokens {
			return SessionUsageSummary{}, errors.New("会话输入 token 累计值溢出")
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
