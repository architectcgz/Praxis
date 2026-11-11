package bindings

import (
	providerapi "praxis/internal/core/provider"
	"praxis/internal/identity"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// ListSessionUsage 保留未上报计数与已知零值的区别。
func (b *AgentBindings) ListSessionUsage(request dto.UsageRequest) ([]dto.ModelUsageRecord, error) {
	if err := validation.Struct(request); err != nil {
		return nil, publicInputError(b.host, "AgentBindings.ListSessionUsage.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	ids := make([]identity.TurnID, len(request.TurnIDs))
	for index, id := range request.TurnIDs {
		ids[index] = identity.TurnID(id)
	}
	records, err := services.Usages.ListSessionUsage(ctx, identity.SessionID(request.SessionID), ids)
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
			SessionID: value.SessionID,
			AgentID:   value.AgentID,
			RunID:     value.RunID,
			RequestID: value.RequestID.String(),
			TurnID:    value.TurnID.String(),
			Usage:     publicUsage(value.Usage),
		})
	}
	return result
}

func publicUsage(value providerapi.Usage) dto.ModelUsage {
	return dto.ModelUsage{
		InputTokens:              value.InputTokens,
		OutputTokens:             value.OutputTokens,
		CacheReadInputTokens:     value.CacheReadInputTokens,
		CacheCreationInputTokens: value.CacheCreationInputTokens,
	}
}
