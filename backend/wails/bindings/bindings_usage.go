package bindings

import (
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// ListSessionUsage 保留未上报计数与已知零值的区别。
func (b *AgentBindings) ListSessionUsage(sessionID string) ([]dto.ModelUsageRecord, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return nil, publicInputError(b.host, "AgentBindings.ListSessionUsage.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	records, err := services.Usages.ListSessionUsage(ctx, identity.SessionID(sessionID))
	if err != nil {
		return nil, publicError(b.host, "AgentBindings.ListSessionUsage", err)
	}
	return usageRecords(records), nil
}

func usageRecords(records []providerapi.UsageRecord) []dto.ModelUsageRecord {
	if records == nil {
		return nil
	}
	result := make([]dto.ModelUsageRecord, 0, len(records))
	for _, value := range records {
		result = append(result, dto.ModelUsageRecord{
			SessionID:   value.SessionID,
			AgentID:     value.AgentID,
			ExecutionID: value.ExecutionID,
			TurnID:      value.TurnID.String(),
			Usage:       publicUsage(value.Usage),
		})
	}
	return result
}

func publicUsage(value providerapi.Usage) dto.ModelUsage {
	return dto.ModelUsage{
		InputTokens:              value.InputTokens,
		OutputTokens:             clonePointer(value.OutputTokens),
		CacheReadInputTokens:     clonePointer(value.CacheReadInputTokens),
		CacheCreationInputTokens: clonePointer(value.CacheCreationInputTokens),
	}
}
