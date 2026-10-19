package bindings

import (
	"strings"

	"praxis/internal/core/toolexecution"
	"praxis/internal/fault"
	"praxis/internal/identity"
	execution "praxis/internal/service/agent/coordinator"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

func (b *CommandBindings) ListToolApprovals(agentID string) ([]dto.ToolApproval, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
		return nil, err
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	values, err := services.Commands.ListToolApprovals(ctx, identity.AgentID(agentID))
	if err != nil {
		return nil, publicError(b.host, "CommandBindings.ListToolApprovals", err)
	}
	result := make([]dto.ToolApproval, 0, len(values))
	for _, value := range values {
		review := value.LatestReview()
		result = append(result, dto.ToolApproval{
			ToolDecisionRequest: dto.ToolDecisionRequest{
				SessionID:       value.SessionID.String(),
				AgentID:         value.AgentID.String(),
				ExecutionID:     value.ExecutionID.String(),
				CallID:          value.ProviderToolCallID,
				ArgumentsDigest: toolexecution.ArgumentsDigest(value.Name, value.NormalizedArguments),
			},
			Name:                string(value.Name),
			NormalizedArguments: string(value.NormalizedArguments),
			Path:                value.Path,
			RuleID:              value.Decision.RuleID,
			Action:              string(value.Decision.Action),
			RiskVerdict:         string(review.Assessment.Verdict),
			RiskReason:          review.Assessment.Reason,
		})
	}
	return result, nil
}

func (b *CommandBindings) ApproveToolCall(wire dto.ToolDecisionRequest) error {
	return b.decideToolCall(wire, true)
}
func (b *CommandBindings) DenyToolCall(wire dto.ToolDecisionRequest) error {
	return b.decideToolCall(wire, false)
}

func (b *CommandBindings) decideToolCall(wire dto.ToolDecisionRequest, approve bool) error {
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	params := execution.ToolDecision{
		SessionID:       identity.SessionID(wire.SessionID),
		AgentID:         identity.AgentID(wire.AgentID),
		ExecutionID:     identity.AgentExecutionID(wire.ExecutionID),
		CallID:          strings.TrimSpace(wire.CallID),
		ArgumentsDigest: strings.TrimSpace(wire.ArgumentsDigest),
	}
	if params.SessionID == "" || params.AgentID == "" || params.ExecutionID == "" || params.CallID == "" || params.ArgumentsDigest == "" {
		err := fault.New(fault.InvalidRequest, "tool approval identity is required")
		return publicError(b.host, "CommandBindings.ToolDecision.request", err)
	}
	if approve {
		err = services.Commands.ApproveToolCall(ctx, params)
	} else {
		err = services.Commands.DenyToolCall(ctx, params)
	}
	if err != nil {
		return publicError(b.host, "CommandBindings.ToolDecision", err)
	}
	return nil
}
