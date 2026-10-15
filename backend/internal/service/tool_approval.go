package service

import (
	"context"
	"encoding/json"

	"praxis/internal/contracts"
	toolmodel "praxis/internal/core/tool_invocation"
	"praxis/internal/request"
)

type ToolApproval struct {
	SessionID       string
	AgentID         string
	TaskID          string
	CallID          string
	Name            string
	ArgumentsDigest string
	RuleID          string
	Command         string
	Path            string
}

func (s *Services) ListToolApprovals(ctx context.Context, agentID contracts.AgentID) ([]ToolApproval, error) {
	values, err := s.runtime.ListToolApprovals(ctx, agentID)
	if err != nil {
		return nil, err
	}
	result := make([]ToolApproval, 0, len(values))
	for _, value := range values {
		var arguments struct {
			Command string `json:"command"`
			Path    string `json:"path"`
		}
		if err := json.Unmarshal(value.NormalizedArguments, &arguments); err != nil {
			return nil, err
		}
		result = append(result, ToolApproval{
			SessionID:       value.SessionID.String(),
			AgentID:         value.AgentID.String(),
			TaskID:          value.TaskID.String(),
			CallID:          value.ProviderToolCallID,
			Name:            string(value.Name),
			ArgumentsDigest: toolmodel.ArgumentsDigest(value.Name, value.NormalizedArguments),
			RuleID:          value.CommandDecision.RuleID,
			Command:         arguments.Command,
			Path:            arguments.Path,
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
