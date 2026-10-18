package bindings

import (
	"context"
	"strings"
	"unicode/utf8"

	"praxis/internal/agent"
	"praxis/internal/fault"
	"praxis/internal/identity"
	modelconfig "praxis/internal/model/config"
	"praxis/internal/task"
	taskservice "praxis/internal/task/service"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

type CommandBindings struct{ runtime Runtime }

func (b *CommandBindings) SendInput(wire dto.SendInputRequest) (dto.SendInputResponse, error) {
	if err := validation.ValidateSendInput(wire); err != nil {
		return dto.SendInputResponse{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SendInputResponse{}, err
	}
	params := taskservice.SendInput{
		SessionID:      identity.SessionID(wire.SessionID),
		AgentID:        identity.AgentID(wire.AgentID),
		RequestID:      identity.RequestID(wire.RequestID),
		Content:        strings.TrimSpace(wire.Content),
		ProviderID:     strings.TrimSpace(wire.ProviderID),
		ModelID:        strings.TrimSpace(wire.ModelID),
		ReasoningLevel: strings.TrimSpace(wire.ReasoningLevel),
	}
	if params.ProviderID == "" || params.ModelID == "" {
		err := fault.New(modelconfig.ModelNotConfigured, "")
		return dto.SendInputResponse{}, publicError(b.runtime, "CommandBindings.SendInput.request", err)
	}
	if params.Content == "" || len(params.Content) > task.MaxInputBytes || !utf8.ValidString(params.Content) {
		err := fault.InvalidValue("content", "content must be non-empty UTF-8 text within the size limit")
		return dto.SendInputResponse{}, publicError(b.runtime, "CommandBindings.SendInput.request", err)
	}
	result, err := services.Commands.SendInput(ctx, params)
	if err != nil {
		return dto.SendInputResponse{}, publicError(b.runtime, "CommandBindings.SendInput", err)
	}
	return dto.SendInputResponse{
		TaskID:          result.Task.ID.String(),
		ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) PauseAgent(wire dto.PauseAgentRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(wire); err != nil {
		return dto.AgentControlResponse{}, err
	}
	return b.controlAgent(func(ctx context.Context, services Services) (agent.ControlResult, error) {
		return services.Agents.PauseAgent(ctx, controlInput(wire))
	})
}

func (b *CommandBindings) CancelTask(wire dto.CancelTaskRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(wire); err != nil {
		return dto.AgentControlResponse{}, err
	}
	return b.controlAgent(func(ctx context.Context, services Services) (agent.ControlResult, error) {
		return services.Agents.StopAgent(ctx, controlInput(wire))
	})
}

// QueueTask 只预约输入，领取和 FIFO 交接由 Execution 负责。
func (b *CommandBindings) QueueTask(wire dto.QueueTaskRequest) (dto.QueueTaskResponse, error) {
	if err := validation.ValidateQueueTask(wire); err != nil {
		return dto.QueueTaskResponse{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.QueueTaskResponse{}, err
	}
	params := taskservice.EnqueueInput{
		ID:             identity.TaskID(wire.TaskID),
		RequestID:      identity.RequestID(wire.RequestID),
		AgentID:        identity.AgentID(wire.AgentID),
		Prompt:         strings.TrimSpace(wire.Prompt),
		ProviderID:     strings.TrimSpace(wire.ProviderID),
		ModelID:        strings.TrimSpace(wire.ModelID),
		ReasoningLevel: strings.TrimSpace(wire.ReasoningLevel),
	}
	if params.ProviderID == "" || params.ModelID == "" {
		err := fault.New(modelconfig.ModelNotConfigured, "")
		return dto.QueueTaskResponse{}, publicError(b.runtime, "CommandBindings.QueueTask.request", err)
	}
	if params.Prompt == "" || len(params.Prompt) > task.MaxInputBytes || !utf8.ValidString(params.Prompt) {
		err := fault.InvalidValue("prompt", "prompt must be non-empty UTF-8 text within the size limit")
		return dto.QueueTaskResponse{}, publicError(b.runtime, "CommandBindings.QueueTask.request", err)
	}
	result, err := services.Commands.EnqueueTask(ctx, params)
	if err != nil {
		return dto.QueueTaskResponse{}, publicError(b.runtime, "CommandBindings.QueueTask", err)
	}
	return dto.QueueTaskResponse{TaskID: result.Task.ID.String(), Status: string(result.Task.Status), ExistingTask: result.ExistingTask}, nil
}

func controlInput(wire dto.PauseAgentRequest) agent.ControlInput {
	return agent.ControlInput{
		CommandID:    identity.AgentControlCommandID(wire.CommandID),
		AgentID:      identity.AgentID(wire.AgentID),
		TargetTaskID: identity.TaskID(wire.TargetTaskID),
	}
}

func (b *CommandBindings) controlAgent(apply func(context.Context, Services) (agent.ControlResult, error)) (dto.AgentControlResponse, error) {
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.AgentControlResponse{}, err
	}
	result, err := apply(ctx, services)
	if err != nil {
		return dto.AgentControlResponse{}, publicError(b.runtime, "CommandBindings.controlAgent", err)
	}
	return dto.AgentControlResponse{
		CommandID:         result.Command.ID.String(),
		AgentID:           result.Command.AgentID.String(),
		TargetTaskID:      result.Command.TargetTaskID.String(),
		Kind:              string(result.Command.Kind),
		Status:            string(result.Command.Status),
		ExistingCommand:   result.ExistingCommand,
		CancellationError: result.CancellationError,
	}, nil
}
