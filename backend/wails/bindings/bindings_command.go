package bindings

import (
	"praxis/internal/contracts"

	"context"
	"praxis/wails/dto"
	"praxis/wails/validation"

	turncontrol "praxis/internal/service/turn/control"
	turnqueue "praxis/internal/service/turn/queue"
	turnstart "praxis/internal/service/turn/start"
)

type CommandBindings struct {
	runtime Runtime
}

func (b *CommandBindings) SendInput(request dto.SendInputRequest) (dto.SendInputResponse, error) {
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SendInputResponse{}, err
	}
	result, err := service.Commands.SendInput(ctx, turnstart.SendInputParams{
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
		TurnID: result.Turn.ID.String(), ExistingRequest: result.ExistingRequest,
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
	result, err := service.Commands.Resume(ctx, turnstart.ResumeParams{
		AgentID: contracts.AgentID(request.AgentID), RequestID: contracts.RequestID(request.RequestID),
		Content: request.Content,
	})
	if err != nil {
		return dto.SendInputResponse{}, publicError(b.runtime, "CommandBindings.Resume", err)
	}
	return dto.SendInputResponse{
		TurnID: result.Turn.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) PauseAgent(request dto.PauseAgentRequest) (dto.AgentControlResponse, error) {
	return b.controlAgent(func(ctx context.Context, service Services) (turncontrol.Result, error) {
		return service.Commands.PauseAgent(ctx, turncontrol.Params{
			CommandID: contracts.AgentControlCommandID(request.CommandID),
			AgentID:   contracts.AgentID(request.AgentID),
		})
	})
}

func (b *CommandBindings) CloseAgent(request dto.CloseAgentRequest) (dto.AgentControlResponse, error) {
	return b.controlAgent(func(ctx context.Context, service Services) (turncontrol.Result, error) {
		return service.Commands.CloseAgent(ctx, turncontrol.Params{
			CommandID: contracts.AgentControlCommandID(request.CommandID),
			AgentID:   contracts.AgentID(request.AgentID),
		})
	})
}

func (b *CommandBindings) QueueWork(request dto.QueueWorkRequest) (dto.QueueWorkResponse, error) {
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.QueueWorkResponse{}, err
	}
	result, err := service.Commands.EnqueueWork(ctx, turnqueue.EnqueueParams{
		ID: contracts.WorkItemID(request.ID), RequestID: contracts.RequestID(request.RequestID),
		AgentID: contracts.AgentID(request.AgentID), Prompt: request.Prompt,
	})
	if err != nil {
		return dto.QueueWorkResponse{}, publicError(b.runtime, "CommandBindings.QueueWork", err)
	}
	return dto.QueueWorkResponse{
		WorkID: result.Work.ID.String(), TurnID: result.Work.TurnID.String(),
		Status: string(result.Work.Status), ExistingWork: result.ExistingWork,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) controlAgent(apply func(context.Context, Services) (turncontrol.Result, error)) (dto.AgentControlResponse, error) {
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
		TargetTurnID: result.Command.TargetTurnID.String(),
		Kind:         string(result.Command.Kind), Status: string(result.Command.Status),
		ExistingCommand: result.ExistingCommand, CancellationError: result.CancellationError,
	}, nil
}
