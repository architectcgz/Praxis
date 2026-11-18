package bindings

import (
	"praxis/internal/dto"
	"praxis/internal/identity"
	"praxis/wails/validation"
)

type AgentBindings struct{ host Host }

// ListAgentHistory 按 Agent 日志游标向前分页。
func (b *AgentBindings) ListAgentHistory(request dto.AgentHistoryRequest) (dto.HistoryPage, error) {
	if request.Limit == 0 {
		request.Limit = 50
	}
	if err := validation.Struct(request); err != nil {
		return dto.HistoryPage{}, publicInputError(b.host, "ListAgentHistory.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.HistoryPage{}, err
	}
	page, err := services.Commands.AgentHistory(ctx, identity.SessionID(request.SessionID), identity.AgentID(request.AgentID), request.Before, request.Limit)
	if err != nil {
		return dto.HistoryPage{}, publicError(b.host, "ListAgentHistory", err)
	}
	return publicHistoryPage(page), nil
}
