package bindings

import (
	"praxis/internal/request"
	"praxis/wails/dto"
)

func (b *CommandBindings) ListToolApprovals(agentID string) ([]dto.ToolApproval, error) {
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	values, err := services.Commands.ListToolApprovals(ctx, agentID)
	if err != nil {
		return nil, publicError(b.runtime, "CommandBindings.ListToolApprovals", err)
	}
	result := make([]dto.ToolApproval, 0, len(values))
	for _, value := range values {
		result = append(result, dto.ToolApproval{
			ToolDecisionRequest: dto.ToolDecisionRequest{
				SessionID:       value.SessionID,
				AgentID:         value.AgentID,
				TaskID:          value.TaskID,
				CallID:          value.CallID,
				ArgumentsDigest: value.ArgumentsDigest,
			},
			Name:                value.Name,
			NormalizedArguments: value.NormalizedArguments,
			Path:                value.Path,
			RuleID:              value.RuleID,
			Action:              value.Action,
			RiskVerdict:         value.RiskVerdict,
			RiskReason:          value.RiskReason,
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
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	canonical, err := request.NewToolDecision(request.ToolDecision{
		SessionID:       wire.SessionID,
		AgentID:         wire.AgentID,
		TaskID:          wire.TaskID,
		CallID:          wire.CallID,
		ArgumentsDigest: wire.ArgumentsDigest,
	})
	if err != nil {
		return publicError(b.runtime, "CommandBindings.ToolDecision.request", err)
	}
	if approve {
		err = services.Commands.ApproveToolCall(ctx, canonical)
	} else {
		err = services.Commands.DenyToolCall(ctx, canonical)
	}
	if err != nil {
		return publicError(b.runtime, "CommandBindings.ToolDecision", err)
	}
	return nil
}
