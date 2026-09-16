package app

import (
	"context"
	"praxis/internal/application/execution/control"
	"praxis/internal/application/execution/queue"
	"praxis/internal/application/execution/start"
	"strings"

	"praxis/internal/contracts"
	domainfoundation "praxis/internal/domain/foundation"
)

type CommandBindings struct {
	runtime  *bindingRuntime
	commands serviceRef[AgentCommands]
}

func (b *CommandBindings) SendInput(
	request contracts.SendInputRequest,
) (response contracts.SendInputResponse, err error) {
	done := b.runtime.begin("SendInput")
	defer func() { done(err) }()
	if strings.TrimSpace(request.ProviderID) == "" || strings.TrimSpace(request.ModelID) == "" {
		return contracts.SendInputResponse{}, bindingError(contracts.ErrorCodeModelNotConfigured)
	}
	ctx, commands, err := b.bindingContext()
	if err != nil {
		return contracts.SendInputResponse{}, err
	}
	result, err := commands.SendInput(ctx, start.SendInputParams{
		SessionID:      domainfoundation.SessionID(request.SessionID),
		AgentID:        domainfoundation.AgentID(request.AgentID),
		RequestID:      domainfoundation.RequestID(request.RequestID),
		Content:        request.Content,
		ProviderID:     request.ProviderID,
		ModelID:        request.ModelID,
		ReasoningLevel: request.ReasoningLevel,
	})
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	return contracts.SendInputResponse{
		ExecutionID: result.Execution.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) Resume(
	request contracts.ResumeRequest,
) (response contracts.SendInputResponse, err error) {
	done := b.runtime.begin("Resume")
	defer func() { done(err) }()
	ctx, commands, err := b.bindingContext()
	if err != nil {
		return contracts.SendInputResponse{}, err
	}
	result, err := commands.Resume(ctx, start.ResumeParams{
		AgentID: domainfoundation.AgentID(request.AgentID), RequestID: domainfoundation.RequestID(request.RequestID),
		Content: request.Content,
	})
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	return contracts.SendInputResponse{
		ExecutionID: result.Execution.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) PauseAgent(request contracts.PauseAgentRequest) (contracts.PauseAgentResponse, error) {
	return b.controlAgent("PauseAgent", request, func(ctx context.Context, commands AgentCommands, params control.Params) (control.Result, error) {
		return commands.PauseAgent(ctx, params)
	})
}

func (b *CommandBindings) CloseAgent(request contracts.CloseAgentRequest) (contracts.CloseAgentResponse, error) {
	return b.controlAgent("CloseAgent", request, func(ctx context.Context, commands AgentCommands, params control.Params) (control.Result, error) {
		return commands.CloseAgent(ctx, params)
	})
}

func (b *CommandBindings) controlAgent(name string, request contracts.PauseAgentRequest, apply func(context.Context, AgentCommands, control.Params) (control.Result, error)) (contracts.AgentControlResponse, error) {
	var err error
	done := b.runtime.begin(name)
	defer func() { done(err) }()
	ctx, commands, err := b.bindingContext()
	if err != nil {
		return contracts.AgentControlResponse{}, err
	}
	result, err := apply(ctx, commands, control.Params{
		CommandID: domainfoundation.AgentControlCommandID(request.CommandID),
		AgentID:   domainfoundation.AgentID(request.AgentID),
	})
	if err != nil {
		return contracts.AgentControlResponse{}, publicBindingError(err)
	}
	return contracts.AgentControlResponse{
		CommandID: result.Command.ID.String(), AgentID: result.Command.AgentID.String(),
		TargetExecutionID: result.Command.TargetExecutionID.String(), Kind: string(result.Command.Kind),
		Status: string(result.Command.Status), ExistingCommand: result.ExistingCommand,
		CancellationError: result.CancellationError,
	}, nil
}

func (b *CommandBindings) QueueWork(
	request contracts.QueueWorkRequest,
) (response contracts.QueueWorkResponse, err error) {
	done := b.runtime.begin("QueueWork")
	defer func() { done(err) }()
	ctx, commands, err := b.bindingContext()
	if err != nil {
		return contracts.QueueWorkResponse{}, err
	}
	result, err := commands.EnqueueWork(ctx, queue.EnqueueParams{
		ID: domainfoundation.WorkItemID(request.ID), RequestID: domainfoundation.RequestID(request.RequestID), AgentID: domainfoundation.AgentID(request.AgentID),
		Prompt: request.Prompt,
	})
	if err != nil {
		return contracts.QueueWorkResponse{}, publicBindingError(err)
	}
	return contracts.QueueWorkResponse{
		WorkID: result.Work.ID.String(), ExecutionID: result.Work.ExecutionID.String(),
		Status: string(result.Work.Status), ExistingWork: result.ExistingWork,
		ActivationError: result.ActivationError,
	}, nil
}

func (b *CommandBindings) bindingContext() (context.Context, AgentCommands, error) {
	ctx, err := b.runtime.context()
	if err != nil {
		return nil, nil, err
	}
	commands := b.commands.get()
	if commands == nil {
		return nil, nil, bindingError(contracts.ErrorCodeBindingUnavailable)
	}
	return ctx, commands, nil
}
