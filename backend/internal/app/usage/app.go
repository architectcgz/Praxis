// Package usage 提供持久化用量的公开查询和快照投影。
package usage

import (
	"context"
	"errors"

	usagecontracts "praxis/internal/app/usage/contracts"
	"praxis/internal/core/identity"
	"praxis/internal/core/model"
	businesssession "praxis/internal/service/session"
)

// App 组合用量查询能力，不改变记录或重新计算业务统计。
type App struct {
	sessions    *businesssession.Service
	listSession func(context.Context, string) ([]model.ModelUsageRecord, error)
}

// New 注入会话业务和只读用量查询，不依赖具体存储实现。
func New(sessions *businesssession.Service, listSession func(context.Context, string) ([]model.ModelUsageRecord, error)) *App {
	return &App{sessions: sessions, listSession: listSession}
}

// ListSession 返回会话完整请求用量，不受消息和计时分页影响。
func (a *App) ListSession(ctx context.Context, sessionID string) ([]usagecontracts.ModelUsageRecord, error) {
	if a.listSession == nil {
		return nil, errors.New("usage query is unavailable")
	}
	records, err := a.listSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	return publicRecords(records), nil
}

// GetSessionUsageSummary 投影业务层的一致用量快照，未知计数仍保留为空。
func (a *App) GetSessionUsageSummary(ctx context.Context, sessionID string) (usagecontracts.SessionUsageSummary, error) {
	summary, err := a.sessions.GetSessionUsageSummary(ctx, identity.SessionID(sessionID))
	if err != nil {
		return usagecontracts.SessionUsageSummary{}, err
	}
	return usagecontracts.SessionUsageSummary{
		Records:              publicRecords(summary.Records),
		InputTokens:          summary.InputTokens,
		CacheReadInputTokens: summary.CacheReadInputTokens,
		CacheReadRatio:       clonePointer(summary.CacheReadRatio),
		CacheReadComplete:    summary.CacheReadComplete,
	}, nil
}

// PublicUsage 复制一次模型用量，避免查询和事件调用方共享可变计数指针。
func PublicUsage(value model.ModelUsage) usagecontracts.ModelUsage {
	return usagecontracts.ModelUsage{
		InputTokens:              value.InputTokens,
		OutputTokens:             clonePointer(value.OutputTokens),
		CacheReadInputTokens:     clonePointer(value.CacheReadInputTokens),
		CacheCreationInputTokens: clonePointer(value.CacheCreationInputTokens),
	}
}

func publicRecords(records []model.ModelUsageRecord) []usagecontracts.ModelUsageRecord {
	if records == nil {
		return nil
	}
	result := make([]usagecontracts.ModelUsageRecord, 0, len(records))
	for _, record := range records {
		result = append(result, usagecontracts.ModelUsageRecord{
			SessionID: record.SessionID,
			AgentID:   record.AgentID,
			TaskID:    record.TaskID,
			TurnID:    record.TurnID.String(),
			Usage:     PublicUsage(record.Usage),
		})
	}
	return result
}

func clonePointer[T any](value *T) *T {
	if value == nil {
		return nil
	}
	copy := *value
	return &copy
}
