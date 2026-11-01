package session

import (
	"cmp"
	"context"
	providerapi "praxis/internal/core/provider"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
)

func (s *Session) ListUsage() []providerapi.UsageRecord {
	s.mu.RLock()
	defer s.mu.RUnlock()
	values := make([]providerapi.UsageRecord, 0, len(s.data.Usage))
	for _, value := range s.data.Usage {
		values = append(values, cloneUsage(value))
	}
	return sorted(values, func(a, b providerapi.UsageRecord) int { return cmp.Compare(a.Key(), b.Key()) })
}

func (s *Session) SaveUsage(ctx context.Context, record providerapi.UsageRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateUsage(s.data, record); err != nil {
		return err
	}
	record = cloneUsage(record)
	s.data.SetUsage(record)
	return s.appendEvent(ctx, EventUsageSaved, "usage", record.Key(), record)
}

func validateUsage(owner *Data, record providerapi.UsageRecord) error {
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
