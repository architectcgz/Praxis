package session

import (
	"cmp"
	"context"
	"errors"
	"math"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

var _ sessionmodel.UsageRecords = (*Service)(nil)

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
func (s *Service) ListSessionUsage(ctx context.Context, sessionID identity.SessionID) (values []providerapi.UsageRecord, err error) {
	owner, err := s.OpenedSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	owner.mu.RLock()
	defer owner.mu.RUnlock()
	if err := owner.checkState(ctx); err != nil {
		return nil, err
	}
	values = make([]providerapi.UsageRecord, 0, len(owner.data.Usage))
	for _, value := range owner.data.Usage {
		values = append(values, cloneUsage(value))
	}
	return sorted(values, func(a, b providerapi.UsageRecord) int { return cmp.Compare(a.Key(), b.Key()) }), nil
}

// GetSessionUsageSummary 查询会话全部最新用量记录，以 token 加权计算缓存率。
// 读取失败、记录归属不一致或累计值溢出时返回错误，未知计数不补零。
func (s *Service) GetSessionUsageSummary(ctx context.Context, sessionID identity.SessionID) (SessionUsageSummary, error) {
	if err := ctx.Err(); err != nil {
		return SessionUsageSummary{}, err
	}
	records, err := s.ListSessionUsage(ctx, sessionID)
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

// SaveUsage 在会话数据写锁内确认归属并保存请求用量。
func (s *Service) SaveUsage(ctx context.Context, record providerapi.UsageRecord) error {
	owner, err := s.OpenedSession(ctx, identity.SessionID(record.SessionID))
	if err != nil {
		return err
	}
	owner.mu.Lock()
	defer owner.mu.Unlock()
	if err := owner.checkState(ctx); err != nil {
		return err
	}
	if err := validateUsage(owner.data, record); err != nil {
		return err
	}
	record = cloneUsage(record)
	if err := owner.appendEvent(ctx, "usage.saved", "usage", record.Key(), record); err != nil {
		return err
	}
	owner.data.SetUsage(record)
	return nil
}

func validateUsage(owner *sessionmodel.Data, record providerapi.UsageRecord) error {
	index := owner.ExecutionIndex(identity.AgentExecutionID(record.ExecutionID))
	if index < 0 {
		return errorcode.ErrNotFound
	}
	execution := owner.Executions[index]
	if execution.AgentID.String() != record.AgentID || execution.AgentID != owner.MainAgentID() {
		return errorcode.ErrRequestConflict
	}
	for _, prior := range owner.Usage {
		if prior.Key() == record.Key() && (prior.AgentID != record.AgentID || prior.ExecutionID != record.ExecutionID || !record.Usage.Includes(prior.Usage)) {
			return errorcode.ErrRequestConflict
		}
	}
	return nil
}

func cloneUsage(value providerapi.UsageRecord) providerapi.UsageRecord {
	if value.Usage.OutputTokens != nil {
		value.Usage.OutputTokens = new(*value.Usage.OutputTokens)
	}
	if value.Usage.CacheReadInputTokens != nil {
		value.Usage.CacheReadInputTokens = new(*value.Usage.CacheReadInputTokens)
	}
	if value.Usage.CacheCreationInputTokens != nil {
		value.Usage.CacheCreationInputTokens = new(*value.Usage.CacheCreationInputTokens)
	}
	return value
}
