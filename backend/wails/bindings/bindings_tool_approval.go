package bindings

import (
	"praxis/internal/identity"
	execution "praxis/internal/service/coordinator"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

func (b *CommandBindings) ListToolApprovals(agentID string) ([]dto.ToolApproval, error) {
	if err := validation.ID("agentId", agentID); err != nil {
		return nil, publicInputError(b.host, "CommandBindings.ListToolApprovals.request", err)
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
		review := value.Review
		result = append(result, dto.ToolApproval{
			ToolDecisionRequest: dto.ToolDecisionRequest{
				SessionID:   value.Call.SessionID.String(),
				AgentID:     value.Call.AgentID.String(),
				ExecutionID: value.Call.ExecutionID.String(),
				CallID:      value.Call.CallID,
			},
			Name:      string(value.Call.Name),
			Arguments: string(value.Arguments),

			RuleID:      value.Decision.RuleID,
			Action:      string(value.Decision.Action),
			RiskVerdict: string(review.Assessment.Verdict),
			RiskReason:  review.Assessment.Reason,
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
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "CommandBindings.ToolDecision.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}

	params := execution.ToolDecision{
		SessionID:   identity.SessionID(wire.SessionID),
		AgentID:     identity.AgentID(wire.AgentID),
		ExecutionID: identity.AgentExecutionID(wire.ExecutionID),
		CallID:      wire.CallID,
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

func (b *CommandBindings) StopToolCall(wire dto.ToolControlRequest) error {
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "CommandBindings.StopToolCall.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	request := execution.ToolControl{
		SessionID:   identity.SessionID(wire.SessionID),
		AgentID:     identity.AgentID(wire.AgentID),
		ExecutionID: identity.AgentExecutionID(wire.ExecutionID),
		CallID:      wire.CallID,
	}
	return publicError(b.host, "CommandBindings.StopToolCall", services.Commands.StopToolCall(ctx, request))
}
