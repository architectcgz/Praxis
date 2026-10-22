package repository

import (
	"context"
	providerapi "praxis/internal/core/provider"
)

// ModelUsage 保存模型请求用量并查询完整的会话用量记录。
type ModelUsage interface {
	Save(context.Context, providerapi.UsageRecord) error
	ListSession(context.Context, string) ([]providerapi.UsageRecord, error)
}
