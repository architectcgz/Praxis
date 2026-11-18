package bindings

import (
	modelusage "praxis/internal/core/model/usage"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/internal/dto"
	"praxis/wails/validation"
)

type AgentBindings struct{ host Host }

// ListAgentEntries 固定目标 Head，以稳定 EntryID 向前分页。
func (b *AgentBindings) ListAgentEntries(request dto.AgentHistoryRequest) (dto.HistoryPage, error) {
	if request.Limit == 0 {
		request.Limit = 50
	}
	if err := validation.Struct(request); err != nil {
		return dto.HistoryPage{}, publicInputError(b.host, "ListAgentEntries.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.HistoryPage{}, err
	}
	page, err := services.Commands.ListAgentEntries(ctx, identity.SessionID(request.SessionID), identity.AgentID(request.AgentID), request.HeadEntryID, request.BeforeEntryID, request.Limit)
	if err != nil {
		return dto.HistoryPage{}, publicError(b.host, "ListAgentEntries", err)
	}
	return publicHistoryPage(page), nil
}

func publicHistoryPage(page sessionmodel.HistoryPage) dto.HistoryPage {
	return dto.HistoryPage{
		HeadEntryID: page.HeadEntryID,
		Entries:     publicEntries(page.Entries),
		HasMore:     page.HasMore,
		NextCursor:  page.NextCursor,
	}
}

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

func usageRecords(records []modelusage.UsageRecord) []dto.ModelUsageRecord {
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

func publicUsage(value modelusage.Usage) dto.ModelUsage {
	return dto.ModelUsage{
		InputTokens:              value.InputTokens,
		OutputTokens:             value.OutputTokens,
		CacheReadInputTokens:     value.CacheReadInputTokens,
		CacheCreationInputTokens: value.CacheCreationInputTokens,
	}
}
