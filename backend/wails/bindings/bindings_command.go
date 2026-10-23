package bindings

import (
	"context"
	"fmt"
	sessionmodel "praxis/internal/core/session"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	agentservice "praxis/internal/service/agent"
	sessionservice "praxis/internal/service/session"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"
	"unicode/utf8"
)

type CommandBindings struct{ host Host }

// CreateTask 只接收 Session 和正文，不要求前端提供 Task、Request 或 Agent 身份。
func (b *CommandBindings) CreateTask(wire dto.CreateTaskRequest) (dto.CreateTaskResponse, error) {
	if err := validation.ValidateSessionID(wire.SessionID); err != nil {
		return dto.CreateTaskResponse{}, publicInputError(b.host, "CommandBindings.CreateTask.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.CreateTaskResponse{}, err
	}
	input := sessionservice.TaskInput{
		SessionID: identity.SessionID(wire.SessionID),
		Prompt:    strings.TrimSpace(wire.Prompt),
	}
	if input.Prompt == "" || len(input.Prompt) > sessionmodel.MaxInputBytes || !utf8.ValidString(input.Prompt) {
		err := fmt.Errorf("prompt: prompt must be non-empty UTF-8 text within the size limit: %w", errorcode.ErrInvalidValue)
		return dto.CreateTaskResponse{}, publicInputError(b.host, "CommandBindings.CreateTask.request", err)
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

func (b *CommandBindings) PauseAgent(wire dto.PauseAgentRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(wire); err != nil {
		return dto.AgentControlResponse{}, publicInputError(b.host, "CommandBindings.PauseAgent.request", err)
	}
	return b.controlAgent(func(ctx context.Context, services Services) (agentservice.ControlResult, error) {
		return services.Agents.PauseAgent(ctx, controlInput(wire))
	})
}

func (b *CommandBindings) StopAgent(wire dto.StopAgentRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(wire); err != nil {
		return dto.AgentControlResponse{}, publicInputError(b.host, "CommandBindings.StopAgent.request", err)
	}
	return b.controlAgent(func(ctx context.Context, services Services) (agentservice.ControlResult, error) {
		return services.Agents.StopAgent(ctx, controlInput(wire))
	})
}

// CancelTask 只移除该 Session 尚未开始的输入，不影响当前 Loop。
func (b *CommandBindings) CancelTask(wire dto.CancelTaskRequest) error {
	if err := validation.ValidateSessionID(wire.SessionID); err != nil {
		return publicInputError(b.host, "CommandBindings.CancelTask.request", err)
	}
	if err := validation.ValidateTaskID(wire.TaskID); err != nil {
		return publicInputError(b.host, "CommandBindings.CancelTask.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	return publicError(b.host, "CommandBindings.CancelTask", services.Commands.CancelTask(ctx, identity.SessionID(wire.SessionID), identity.TaskID(wire.TaskID)))
}

func controlInput(wire dto.PauseAgentRequest) agentservice.ControlInput {
	return agentservice.ControlInput{
		CommandID:         identity.AgentControlCommandID(wire.CommandID),
		AgentID:           identity.AgentID(wire.AgentID),
		TargetExecutionID: identity.AgentExecutionID(wire.TargetExecutionID),
	}
}

func (b *CommandBindings) controlAgent(apply func(context.Context, Services) (agentservice.ControlResult, error)) (dto.AgentControlResponse, error) {
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.AgentControlResponse{}, err
	}
	result, err := apply(ctx, services)
	if err != nil {
		return dto.AgentControlResponse{}, publicError(b.host, "CommandBindings.controlAgent", err)
	}
	return dto.AgentControlResponse{
		CommandID:         result.Command.ID.String(),
		AgentID:           result.Command.AgentID.String(),
		TargetExecutionID: result.Command.TargetExecutionID.String(),
		Kind:              string(result.Command.Kind),
		Status:            string(result.Command.Status),
		ExistingCommand:   result.ExistingCommand,
		CancellationError: result.CancellationError,
	}, nil
}
