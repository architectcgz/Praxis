package bindings

import (
	"praxis/internal/service"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// ListSessionUsage 返回会话所有请求的已上报用量；未上报的计数不补零。
func (b *AgentBindings) ListSessionUsage(sessionID string) ([]dto.ModelUsageRecord, error) {
	if err := validation.ValidateSessionID(sessionID); err != nil {
		return nil, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	records, err := services.Usages.ListSession(ctx, sessionID)
	if err != nil {
		return nil, publicError(b.runtime, "AgentBindings.ListSessionUsage", err)
	}
	return usageRecords(records), nil
}

func usageRecords(records []service.ModelUsageRecord) []dto.ModelUsageRecord {
	if records == nil {
		return nil
	}
	result := make([]dto.ModelUsageRecord, 0, len(records))
	for _, record := range records {
		result = append(result, dto.ModelUsageRecord{
			SessionID: record.SessionID,
			AgentID:   record.AgentID,
			TaskID:    record.TaskID,
			TurnID:    record.TurnID.String(),
			Usage: dto.ModelUsage{
				InputTokens:              record.Usage.InputTokens,
				OutputTokens:             record.Usage.OutputTokens,
				CacheReadInputTokens:     record.Usage.CacheReadInputTokens,
				CacheCreationInputTokens: record.Usage.CacheCreationInputTokens,
			},
		})
	}
	return result
}
