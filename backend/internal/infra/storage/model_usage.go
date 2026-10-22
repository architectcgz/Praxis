package storage

import (
	"context"
	"errors"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/fault"
	"praxis/internal/identity"
)

// ModelUsageStore 将请求用量保存在所属 Session 日志中，与消息和计时分别查询。
type ModelUsageStore struct{ Store *Store }

// Save 按请求身份幂等保存累计计数；拒绝身份变更、计数回退和已知字段丢失。
func (r ModelUsageStore) Save(ctx context.Context, value providerapi.UsageRecord) error {
	return r.Store.view(ctx, func() error {
		previous, err := getLocked[providerapi.UsageRecord](r.Store, "usage", value.Key())
		if err == nil {
			if previous.SessionID != value.SessionID || previous.AgentID != value.AgentID || previous.Usage.InputTokens > value.Usage.InputTokens {
				return fault.ErrRequestConflict
			}
			for _, counts := range [][2]*int64{
				{previous.Usage.OutputTokens, value.Usage.OutputTokens},
				{previous.Usage.CacheReadInputTokens, value.Usage.CacheReadInputTokens},
				{previous.Usage.CacheCreationInputTokens, value.Usage.CacheCreationInputTokens},
			} {
				if counts[0] != nil && (counts[1] == nil || *counts[1] < *counts[0]) {
					return fault.ErrRequestConflict
				}
			}
		} else if !errors.Is(err, fault.ErrNotFound) {
			return err
		}
		return r.Store.appendLocked(ctx, value.SessionID, "usage", value.Key(), "usage.saved", value)
	})
}

// ListSession 返回整个会话所有 Agent 的请求用量，不受消息历史分页限制。
func (r ModelUsageStore) ListSession(ctx context.Context, sessionID string) ([]providerapi.UsageRecord, error) {
	if _, err := (SessionRepository{r.Store}).Get(ctx, identity.SessionID(sessionID)); err != nil {
		return nil, err
	}
	return listIndexed[providerapi.UsageRecord](ctx, r.Store, lookupKey{kind: "usage-session", owner: sessionID}, nil)
}
