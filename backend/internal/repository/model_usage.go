package repository

import (
	"context"
	"praxis/internal/core/model"
)

// ModelUsage 保存模型请求用量并查询完整的会话用量记录。
type ModelUsage interface {
	Save(context.Context, model.ModelUsageRecord) error
	ListSession(context.Context, string) ([]model.ModelUsageRecord, error)
}
