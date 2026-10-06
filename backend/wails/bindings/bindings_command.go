package bindings

import (
	"praxis/internal/contracts"

	"context"
	applicationagent "praxis/internal/service/agent"
	"praxis/wails/dto"
	"praxis/wails/validation"

	taskqueue "praxis/internal/service/runtime/queue"
	taskstart "praxis/internal/service/runtime/task/start"
)

type CommandBindings struct {
	runtime Runtime
}

func (b *CommandBindings) SendInput(request dto.SendInputRequest) (dto.SendInputResponse, error) {
	if err := validation.ValidateSendInput(request); err != nil {
		return dto.SendInputResponse{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SendInputResponse{}, err
	}
	result, err := service.Commands.SendInput(ctx, taskstart.SendInputParams{
		SessionID:      contracts.SessionID(request.SessionID),
		AgentID:        contracts.AgentID(request.AgentID),
		RequestID:      contracts.RequestID(request.RequestID),
		Content:        request.Content,
		ProviderID:     request.ProviderID,
		ModelID:        request.ModelID,
		ReasoningLevel: request.ReasoningLevel,
	})
	if err != nil {
		return dto.SendInputResponse{}, publicError(b.runtime, "CommandBindings.SendInput", err)
	}
	return dto.SendInputResponse{
		TaskID: result.Task.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) PauseAgent(request dto.PauseAgentRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(request); err != nil {
		return dto.AgentControlResponse{}, err
	}
	return b.controlAgent(func(ctx context.Context, service Services) (applicationagent.ControlResult, error) {
		return service.Commands.PauseAgent(ctx, applicationagent.ControlParams{
			CommandID:    contracts.AgentControlCommandID(request.CommandID),
			AgentID:      contracts.AgentID(request.AgentID),
			TargetTaskID: contracts.TaskID(request.TargetTaskID),
		})
	})
}

func (b *CommandBindings) CancelTask(request dto.CancelTaskRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(request); err != nil {
		return dto.AgentControlResponse{}, err
	}
	return b.controlAgent(func(ctx context.Context, service Services) (applicationagent.ControlResult, error) {
		return service.Commands.CancelTask(ctx, applicationagent.ControlParams{
			CommandID:    contracts.AgentControlCommandID(request.CommandID),
			AgentID:      contracts.AgentID(request.AgentID),
			TargetTaskID: contracts.TaskID(request.TargetTaskID),
		})
	})
}

// QueueTask 在执行期间预约输入，返回后续执行会继续使用的 Task ID。
func (b *CommandBindings) QueueTask(request dto.QueueTaskRequest) (dto.QueueTaskResponse, error) {
	if err := validation.ValidateQueueTask(request); err != nil {
		return dto.QueueTaskResponse{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.QueueTaskResponse{}, err
	}
	result, err := service.Commands.EnqueueTask(ctx, taskqueue.EnqueueParams{
		ID:             contracts.TaskID(request.TaskID),
		RequestID:      contracts.RequestID(request.RequestID),
		AgentID:        contracts.AgentID(request.AgentID),
		Prompt:         request.Prompt,
		ProviderID:     request.ProviderID,
		ModelID:        request.ModelID,
		ReasoningLevel: request.ReasoningLevel,
	})
	if err != nil {
		return dto.QueueTaskResponse{}, publicError(b.runtime, "CommandBindings.QueueTask", err)
	}
	return dto.QueueTaskResponse{
		TaskID:       result.Task.ID.String(),
		Status:       string(result.Task.Status),
		ExistingTask: result.ExistingTask,
	}, nil
}

func (b *CommandBindings) controlAgent(apply func(context.Context, Services) (applicationagent.ControlResult, error)) (dto.AgentControlResponse, error) {
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.AgentControlResponse{}, err
	}
	result, err := apply(ctx, service)
	if err != nil {
		return dto.AgentControlResponse{}, publicError(b.runtime, "CommandBindings.controlAgent", err)
	}
	return dto.AgentControlResponse{
		CommandID: result.Command.ID.String(), AgentID: result.Command.AgentID.String(),
		TargetTaskID: result.Command.TargetTaskID.String(),
		Kind:         string(result.Command.Kind), Status: string(result.Command.Status),
		ExistingCommand: result.ExistingCommand, CancellationError: result.CancellationError,
	}, nil
}
