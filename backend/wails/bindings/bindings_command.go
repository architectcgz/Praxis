package bindings

import (
	"praxis/internal/identity"
	sessionservice "praxis/internal/service/session"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
)

type CommandBindings struct{ host Host }

// CreateTask 在首次提交时取得写入绑定，并返回实际写入的分支。
func (b *CommandBindings) CreateTask(wire dto.CreateTaskRequest) (dto.CreateTaskResponse, error) {
	wire.Prompt = strings.TrimSpace(wire.Prompt)
	if err := validation.Struct(wire); err != nil {
		return dto.CreateTaskResponse{}, publicInputError(b.host, "CommandBindings.CreateTask.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.CreateTaskResponse{}, err
	}
	input := sessionservice.TaskInput{
		BindingID:    wire.BindingID,
		BranchID:     identity.BranchID(wire.BranchID),
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
		BindingID:       result.BindingID,
		BranchID:        result.BranchID.String(),
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
	return publicError(b.host, "CommandBindings.StopAgent", services.Commands.StopAgent(ctx, identity.AgentID(wire.AgentID)))
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
	return publicError(b.host, "CommandBindings.CancelTask", services.Commands.CancelTask(ctx, wire.BindingID, identity.TaskID(wire.TaskID)))
}
