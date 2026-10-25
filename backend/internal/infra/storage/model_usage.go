package storage

import (
	"context"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
)

// ModelUsageRepository 只保存 Main 的请求用量，不展开子执行过程。
type ModelUsageRepository struct{ storage *Storage }

func (r ModelUsageRepository) Save(ctx context.Context, value providerapi.UsageRecord) error {
	return save(ctx, r.storage, value.SessionID, "usage", value.Key(), "usage.saved", value)
}

func (r ModelUsageRepository) ListSession(ctx context.Context, sessionID string) (result []providerapi.UsageRecord, err error) {
	err = withState(ctx, r.storage, func(_ *Storage, records []logRecord) error {
		if _, err := getRecord[sessionmodel.Session](records, "session", sessionID); err != nil {
			return err
		}
		result, err = listRecords(records, "usage", func(value providerapi.UsageRecord) bool { return value.SessionID == sessionID })
		return err
	})
	return result, err
}
