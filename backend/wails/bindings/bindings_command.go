package bindings

import (
	"context"

	"praxis/internal/contracts"
	"praxis/internal/request"
	"praxis/internal/service"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

type CommandBindings struct {
	runtime Runtime
}

func (b *CommandBindings) SendInput(wire dto.SendInputRequest) (dto.SendInputResponse, error) {
	if err := validation.ValidateSendInput(wire); err != nil {
		return dto.SendInputResponse{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SendInputResponse{}, err
	}
	canonical, err := request.NewSendInput(request.SendInput{
		SessionID:      contracts.SessionID(wire.SessionID),
		AgentID:        contracts.AgentID(wire.AgentID),
		RequestID:      contracts.RequestID(wire.RequestID),
		Content:        wire.Content,
		ProviderID:     wire.ProviderID,
		ModelID:        wire.ModelID,
		ReasoningLevel: wire.ReasoningLevel,
	})
	if err != nil {
		return dto.SendInputResponse{}, publicError(b.runtime, "CommandBindings.SendInput.request", err)
	}
	result, err := services.Commands.SendInput(ctx, canonical)
	if err != nil {
		return dto.SendInputResponse{}, publicError(b.runtime, "CommandBindings.SendInput", err)
	}
	return dto.SendInputResponse{
		TaskID:          result.TaskID.String(),
		ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) PauseAgent(wire dto.PauseAgentRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(wire); err != nil {
		return dto.AgentControlResponse{}, err
	}
	return b.controlAgent(func(ctx context.Context, services Services) (service.ControlResult, error) {
		return services.Commands.PauseAgent(ctx, request.Control{
			CommandID:    contracts.AgentControlCommandID(wire.CommandID),
			AgentID:      contracts.AgentID(wire.AgentID),
			TargetTaskID: contracts.TaskID(wire.TargetTaskID),
		})
	})
}

func (b *CommandBindings) CancelTask(wire dto.CancelTaskRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(wire); err != nil {
		return dto.AgentControlResponse{}, err
	}
	return b.controlAgent(func(ctx context.Context, services Services) (service.ControlResult, error) {
		return services.Commands.CancelTask(ctx, request.Control{
			CommandID:    contracts.AgentControlCommandID(wire.CommandID),
			AgentID:      contracts.AgentID(wire.AgentID),
			TargetTaskID: contracts.TaskID(wire.TargetTaskID),
		})
	})
}

// QueueTask 在执行期间预约输入，返回后续执行会继续使用的 Task ID。
func (b *CommandBindings) QueueTask(wire dto.QueueTaskRequest) (dto.QueueTaskResponse, error) {
	if err := validation.ValidateQueueTask(wire); err != nil {
		return dto.QueueTaskResponse{}, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.QueueTaskResponse{}, err
	}
	canonical, err := request.NewEnqueueTask(request.EnqueueTask{
		ID:             contracts.TaskID(wire.TaskID),
		RequestID:      contracts.RequestID(wire.RequestID),
		AgentID:        contracts.AgentID(wire.AgentID),
		Prompt:         wire.Prompt,
		ProviderID:     wire.ProviderID,
		ModelID:        wire.ModelID,
		ReasoningLevel: wire.ReasoningLevel,
	})
	if err != nil {
		return dto.QueueTaskResponse{}, publicError(b.runtime, "CommandBindings.QueueTask.request", err)
	}
	result, err := services.Commands.EnqueueTask(ctx, canonical)
	if err != nil {
		return dto.QueueTaskResponse{}, publicError(b.runtime, "CommandBindings.QueueTask", err)
	}
	return dto.QueueTaskResponse{
		TaskID:       result.TaskID.String(),
		Status:       result.Status,
		ExistingTask: result.ExistingTask,
	}, nil
}

func (b *CommandBindings) controlAgent(apply func(context.Context, Services) (service.ControlResult, error)) (dto.AgentControlResponse, error) {
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
		Kind:              result.Command.Kind,
		Status:            result.Command.Status,
		ExistingCommand:   result.ExistingCommand,
		CancellationError: result.CancellationError,
	}, nil
}
