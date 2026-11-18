package bindings

import (
	execution "praxis/internal/core/coordinator"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/identity"
	"praxis/internal/dto"
	"praxis/wails/validation"
	"strings"
)

type CommandBindings struct{ host Host }

// CreateTask 在首次提交时取得会话写入绑定，后续输入复用同一绑定。
func (b *CommandBindings) CreateTask(wire dto.CreateTaskRequest) (dto.CreateTaskResponse, error) {
	wire.Prompt = strings.TrimSpace(wire.Prompt)
	if err := validation.Struct(wire); err != nil {
		return dto.CreateTaskResponse{}, publicInputError(b.host, "CommandBindings.CreateTask.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.CreateTaskResponse{}, err
	}
	input := taskmodel.Input{
		SubmissionID: identity.TaskID(wire.SubmissionID),
		SessionID:    identity.SessionID(wire.SessionID),
		Prompt:       wire.Prompt,
	}
	result, err := services.Commands.SubmitTask(ctx, input)
	if err != nil {
		return dto.CreateTaskResponse{}, publicError(b.host, "CommandBindings.CreateTask", err)
	}
	return dto.CreateTaskResponse{
		TaskID:          result.Task.ID.String(),
		SchedulingError: result.SchedulingError,
	}, nil
}

func (b *CommandBindings) StopAgent(wire dto.StopAgentRequest) error {
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "CommandBindings.StopAgent.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "CommandBindings.StopAgent", services.Commands.StopAgent(ctx, execution.AgentControl{
		SessionID: identity.SessionID(wire.SessionID),
		AgentID:   identity.AgentID(wire.AgentID),
		RunID:     identity.RunID(wire.RunID),
	}))
}

// CancelTask 只移除该 Session 尚未开始的输入，不影响当前 Loop。
func (b *CommandBindings) CancelTask(wire dto.CancelTaskRequest) error {
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "CommandBindings.CancelTask.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "CommandBindings.CancelTask", services.Commands.CancelTask(ctx, identity.SessionID(wire.SessionID), identity.TaskID(wire.TaskID)))
}

func (b *CommandBindings) PauseQueue(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "PauseQueue", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "PauseQueue", services.Commands.PauseQueue(ctx, identity.SessionID(sessionID)))
}

func (b *CommandBindings) ResumeQueue(sessionID string) error {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return publicInputError(b.host, "ResumeQueue", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "ResumeQueue", services.Commands.ResumeQueue(ctx, identity.SessionID(sessionID)))
}

func (b *CommandBindings) ListToolApprovals(sessionID, agentID string) ([]dto.ToolApproval, error) {
	if err := validation.Value("sessionId", sessionID, "session_id"); err != nil {
		return nil, publicInputError(b.host, "CommandBindings.ListToolApprovals.request", err)
	}
	if err := validation.Value("agentId", agentID, "id"); err != nil {
		return nil, publicInputError(b.host, "CommandBindings.ListToolApprovals.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	values, err := services.Commands.ListToolApprovals(ctx, identity.SessionID(sessionID), identity.AgentID(agentID))
	if err != nil {
		return nil, publicError(b.host, "CommandBindings.ListToolApprovals", err)
	}
	result := make([]dto.ToolApproval, 0, len(values))
	for _, value := range values {
		review := value.Review
		result = append(result, dto.ToolApproval{
			ToolDecisionRequest: dto.ToolDecisionRequest{
				SessionID: value.Call.SessionID.String(),
				AgentID:   value.Call.AgentID.String(),
				RunID:     value.Call.RunID.String(),
				CallID:    value.Call.CallID,
			},
			Name:        string(value.Call.Name),
			Arguments:   string(value.Arguments),
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
		SessionID: identity.SessionID(wire.SessionID),
		AgentID:   identity.AgentID(wire.AgentID),
		RunID:     identity.RunID(wire.RunID),
		CallID:    wire.CallID,
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

func (b *CommandBindings) StopToolCall(wire dto.ToolCancelRequest) error {
	if err := validation.Struct(wire); err != nil {
		return publicInputError(b.host, "CommandBindings.StopToolCall.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	request := execution.ToolControl{
		SessionID: identity.SessionID(wire.SessionID),
		AgentID:   identity.AgentID(wire.AgentID),
		CallID:    wire.CallID,
	}
	return publicError(b.host, "CommandBindings.StopToolCall", services.Commands.StopToolCall(ctx, request))
}
