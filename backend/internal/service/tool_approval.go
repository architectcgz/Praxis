package service

import (
	"context"
	"encoding/json"
	"praxis/internal/contracts"
	"praxis/internal/request"
)

type ToolApproval struct {
	SessionID string
	AgentID   string
	TaskID    string
	CallID    string
	Name      string
	Arguments json.RawMessage
}

func (s *Services) ListToolApprovals(ctx context.Context, agentID contracts.AgentID) ([]ToolApproval, error) {
	values, err := s.runtime.ListToolApprovals(ctx, agentID)
	if err != nil {
		return nil, err
	}
	result := make([]ToolApproval, 0, len(values))
	for _, value := range values {
		result = append(result, ToolApproval{
			SessionID: value.SessionID.String(),
			AgentID:   value.AgentID.String(),
			TaskID:    value.TaskID.String(),
			CallID:    value.ProviderToolCallID,
			Name:      string(value.Name),
			Arguments: append(json.RawMessage(nil), value.NormalizedArguments...),
		})
	}
	return result, nil
}
func (s *Services) ApproveToolCall(ctx context.Context, params request.ToolDecision) error {
	return s.runtime.ApproveToolCall(ctx, params)
}
func (s *Services) DenyToolCall(ctx context.Context, params request.ToolDecision) error {
	return s.runtime.DenyToolCall(ctx, params)
}
