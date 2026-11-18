package bindings

import (
	"praxis/internal/identity"
	"praxis/wails/validation"
)

type AgentBindings struct{ host Host }

const defaultHistoryLimit = 50

type AgentHistoryRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	AgentID   string `json:"agentId" validate:"id"`
	Before    *int64 `json:"before" validate:"omitempty,min=0"`
	Limit     int    `json:"limit" validate:"min=1,max=200"`
}

// ListAgentHistory 按 Agent 日志游标向前分页。
func (b *AgentBindings) ListAgentHistory(request AgentHistoryRequest) (HistoryPage, error) {
	if request.Limit == 0 {
		request.Limit = defaultHistoryLimit
	}
	if err := validation.Struct(request); err != nil {
		return HistoryPage{}, publicInputError(b.host, "ListAgentHistory.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return HistoryPage{}, err
	}
	page, err := services.Commands.AgentHistory(ctx, identity.SessionID(request.SessionID), identity.AgentID(request.AgentID), request.Before, request.Limit)
	if err != nil {
		return HistoryPage{}, publicError(b.host, "ListAgentHistory", err)
	}
	return publicHistoryPage(page), nil
}
