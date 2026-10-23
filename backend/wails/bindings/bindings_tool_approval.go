package bindings

import (
	"fmt"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	execution "praxis/internal/service/agent/coordinator"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

func (b *CommandBindings) ListToolApprovals(agentID string) ([]dto.ToolApproval, error) {
	if err := validation.ValidateAgentID(agentID); err != nil {
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
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}

	params := execution.ToolDecision{
		SessionID:   identity.SessionID(wire.SessionID),
		AgentID:     identity.AgentID(wire.AgentID),
		ExecutionID: identity.AgentExecutionID(wire.ExecutionID),
		CallID:      strings.TrimSpace(wire.CallID),
	}
	if params.SessionID == "" || params.AgentID == "" || params.ExecutionID == "" || params.CallID == "" {
		err := fmt.Errorf("tool approval identity is required: %w", errorcode.ErrInvalidRequest)
		return publicInputError(b.host, "CommandBindings.ToolDecision.request", err)
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
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	request := execution.ToolControl{
		SessionID:   identity.SessionID(strings.TrimSpace(wire.SessionID)),
		AgentID:     identity.AgentID(strings.TrimSpace(wire.AgentID)),
		ExecutionID: identity.AgentExecutionID(strings.TrimSpace(wire.ExecutionID)),
		CallID:      strings.TrimSpace(wire.CallID),
	}
	if request.SessionID == "" || request.AgentID == "" || request.ExecutionID == "" || request.CallID == "" {
		return publicInputError(b.host, "CommandBindings.StopToolCall.request", fmt.Errorf("tool control identity is required: %w", errorcode.ErrInvalidRequest))
	}
	return publicError(b.host, "CommandBindings.StopToolCall", services.Commands.StopToolCall(ctx, request))
}
