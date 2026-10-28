package storage

import (
	"cmp"
	"context"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

type ModelUsageRepository struct{ storage *Store }

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
func (r ModelUsageRepository) Save(ctx context.Context, value providerapi.UsageRecord) error {
	if err := r.storage.lock(ctx); err != nil {
		return err
	}
	defer r.storage.mu.Unlock()
	value = cloneUsage(value)
	if err := r.storage.appendEventLocked(ctx, identity.SessionID(value.SessionID), "usage.saved", "usage", value.Key(), value); err != nil {
		return err
	}
	r.storage.state.usage[value.Key()] = value
	return nil
}
func (r ModelUsageRepository) ListSession(ctx context.Context, sessionID string) ([]providerapi.UsageRecord, error) {
	if err := r.storage.readLock(ctx); err != nil {
		return nil, err
	}
	defer r.storage.mu.RUnlock()
	if _, ok := r.storage.state.sessions[identity.SessionID(sessionID)]; !ok {
		return nil, errorcode.ErrNotFound
	}
	result := make([]providerapi.UsageRecord, 0)
	for _, value := range r.storage.state.usage {
		if value.SessionID == sessionID {
			result = append(result, cloneUsage(value))
		}
	}
	return sorted(result, func(a, b providerapi.UsageRecord) int { return cmp.Compare(a.Key(), b.Key()) }), nil
}
