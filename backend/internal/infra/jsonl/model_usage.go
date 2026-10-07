package jsonl

import (
	"context"
	"errors"

	"praxis/internal/contracts"
	"praxis/internal/core/model"
)

// ModelUsageStore 将请求用量保存在所属 Session 日志中，与消息和计时分别查询。
type ModelUsageStore struct{ Store *Store }

// Save 按请求身份幂等保存累计计数；拒绝身份变更、计数回退和已知字段丢失。
func (r ModelUsageStore) Save(ctx context.Context, value model.ModelUsageRecord) error {
	return r.Store.InTx(ctx, func(ctx context.Context) error {
		previous, err := get[model.ModelUsageRecord](ctx, r.Store, "usage", value.Key())
		if err == nil {
			if previous.SessionID != value.SessionID || previous.AgentID != value.AgentID || previous.Usage.InputTokens > value.Usage.InputTokens {
				return contracts.ErrRequestConflict
			}
			for _, counts := range [][2]*int64{
				{previous.Usage.OutputTokens, value.Usage.OutputTokens},
				{previous.Usage.CacheReadInputTokens, value.Usage.CacheReadInputTokens},
				{previous.Usage.CacheCreationInputTokens, value.Usage.CacheCreationInputTokens},
			} {
				if counts[0] != nil && (counts[1] == nil || *counts[1] < *counts[0]) {
					return contracts.ErrRequestConflict
				}
			}
		} else if !errors.Is(err, contracts.ErrNotFound) {
			return err
		}
		return save(ctx, r.Store, value.SessionID, "usage", value.Key(), "usage.saved", value)
	})
}

// ListSession 返回整个会话所有 Agent 的请求用量，不受消息历史分页限制。
func (r ModelUsageStore) ListSession(ctx context.Context, sessionID string) ([]model.ModelUsageRecord, error) {
	if _, err := (SessionRepository{r.Store}).Get(ctx, contracts.SessionID(sessionID)); err != nil {
		return nil, err
	}
	return list[model.ModelUsageRecord](ctx, r.Store, "usage", func(value model.ModelUsageRecord) bool {
		return value.SessionID == sessionID
	})
}
