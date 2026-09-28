package bindings

import (
	"praxis/internal/contracts"

	"context"
	"praxis/wails/dto"
	"praxis/wails/validation"
	"strings"

	executioncontrol "praxis/internal/service/execution/control"
	executionqueue "praxis/internal/service/execution/queue"
	executionstart "praxis/internal/service/execution/start"
)

type CommandBindings struct {
	runtime Runtime
}

func (b *CommandBindings) SendInput(request dto.SendInputRequest) (dto.SendInputResponse, error) {
	if err := validation.ValidateSendInput(request); err != nil {
		return dto.SendInputResponse{}, err
	}
	if strings.TrimSpace(request.ProviderID) == "" || strings.TrimSpace(request.ModelID) == "" {
		return dto.SendInputResponse{}, publicError(b.runtime, "CommandBindings.SendInput.model", contracts.New(contracts.ModelNotConfigured, "model is not configured"))
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SendInputResponse{}, err
	}
	result, err := service.Commands.SendInput(ctx, executionstart.SendInputParams{
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
		ExecutionID: result.Execution.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) Resume(request dto.ResumeRequest) (dto.SendInputResponse, error) {
	if err := validation.ValidateResume(request); err != nil {
		return dto.SendInputResponse{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SendInputResponse{}, err
	}
	result, err := service.Commands.Resume(ctx, executionstart.ResumeParams{
		AgentID: contracts.AgentID(request.AgentID), RequestID: contracts.RequestID(request.RequestID),
		Content: request.Content,
	})
	if err != nil {
		return dto.SendInputResponse{}, publicError(b.runtime, "CommandBindings.Resume", err)
	}
	return dto.SendInputResponse{
		ExecutionID: result.Execution.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) PauseAgent(request dto.PauseAgentRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(request); err != nil {
		return dto.AgentControlResponse{}, err
	}
	return b.controlAgent(func(ctx context.Context, service Services) (executioncontrol.Result, error) {
		return service.Commands.PauseAgent(ctx, executioncontrol.Params{
			CommandID: contracts.AgentControlCommandID(request.CommandID),
			AgentID:   contracts.AgentID(request.AgentID),
		})
	})
}

func (b *CommandBindings) CloseAgent(request dto.CloseAgentRequest) (dto.AgentControlResponse, error) {
	if err := validation.ValidateAgentControl(request); err != nil {
		return dto.AgentControlResponse{}, err
	}
	return b.controlAgent(func(ctx context.Context, service Services) (executioncontrol.Result, error) {
		return service.Commands.CloseAgent(ctx, executioncontrol.Params{
			CommandID: contracts.AgentControlCommandID(request.CommandID),
			AgentID:   contracts.AgentID(request.AgentID),
		})
	})
}

func (b *CommandBindings) QueueWork(request dto.QueueWorkRequest) (dto.QueueWorkResponse, error) {
	if err := validation.ValidateQueueWork(request); err != nil {
		return dto.QueueWorkResponse{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.QueueWorkResponse{}, err
	}
	result, err := service.Commands.EnqueueWork(ctx, executionqueue.EnqueueParams{
		ID: contracts.WorkItemID(request.ID), RequestID: contracts.RequestID(request.RequestID),
		AgentID: contracts.AgentID(request.AgentID), Prompt: request.Prompt,
	})
	if err != nil {
		return dto.QueueWorkResponse{}, publicError(b.runtime, "CommandBindings.QueueWork", err)
	}
	return dto.QueueWorkResponse{
		WorkID: result.Work.ID.String(), ExecutionID: result.Work.ExecutionID.String(),
		Status: string(result.Work.Status), ExistingWork: result.ExistingWork,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) controlAgent(apply func(context.Context, Services) (executioncontrol.Result, error)) (dto.AgentControlResponse, error) {
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
		TargetExecutionID: result.Command.TargetExecutionID.String(),
		Kind:              string(result.Command.Kind), Status: string(result.Command.Status),
		ExistingCommand: result.ExistingCommand, CancellationError: result.CancellationError,
	}, nil
}
