package service

import (
	"context"

	"praxis/internal/contracts"
	toolmodel "praxis/internal/core/tool_execution"
	"praxis/internal/request"
)

type ToolApproval struct {
	SessionID           string
	AgentID             string
	TaskID              string
	CallID              string
	Name                string
	ArgumentsDigest     string
	RuleID              string
	NormalizedArguments string
	Path                string
	Action              string
	RiskVerdict         string
	RiskReason          string
}

func (s *Services) ListToolApprovals(ctx context.Context, agentID contracts.AgentID) ([]ToolApproval, error) {
	values, err := s.runtime.ListToolApprovals(ctx, agentID)
	if err != nil {
		return nil, err
	}
	result := make([]ToolApproval, 0, len(values))
	for _, value := range values {
		review := value.LatestReview()
		result = append(result, ToolApproval{
			SessionID:           value.SessionID.String(),
			AgentID:             value.AgentID.String(),
			TaskID:              value.TaskID.String(),
			CallID:              value.ProviderToolCallID,
			Name:                string(value.Name),
			ArgumentsDigest:     toolmodel.ArgumentsDigest(value.Name, value.NormalizedArguments),
			RuleID:              value.Decision.RuleID,
			NormalizedArguments: string(value.NormalizedArguments),
			Path:                value.Path,
			Action:              string(value.Decision.Action),
			RiskVerdict:         string(review.Assessment.Verdict),
			RiskReason:          review.Assessment.Reason,
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
