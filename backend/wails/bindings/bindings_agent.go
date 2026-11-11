package bindings

import (
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/identity"
	"praxis/wails/dto"
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
	page, err := services.Commands.ListAgentEntries(ctx, identity.SessionID(request.SessionID), identity.AgentID(request.AgentID), identity.BranchID(request.BranchID), request.HeadEntryID, request.BeforeEntryID, request.Limit)
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

func validateAgentQuery(sessionID, branchID, agentID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return err
	}
	if err := validation.ID("branchId", branchID); err != nil {
		return err
	}
	return validation.ID("agentId", agentID)
}
