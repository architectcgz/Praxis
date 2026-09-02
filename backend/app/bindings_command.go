package app

import (
	"context"
	domainworkflow "praxis/internal/core/domain/workflow"
	"strings"

	"praxis/internal/contracts"
	domainfoundation "praxis/internal/core/domain/foundation"

	"praxis/internal/core/orchestrate"
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
	result, err := commands.SendInput(ctx, orchestrate.SendInputRequest{
		SessionID:  domainfoundation.SessionID(request.SessionID),
		AgentID:    domainfoundation.AgentID(request.AgentID),
		RequestID:  domainfoundation.RequestID(request.RequestID),
		Content:    request.Content,
		ProviderID: request.ProviderID,
		ModelID:    request.ModelID,
		Reasoning:  request.Reasoning,
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
	result, err := commands.Resume(ctx, orchestrate.ResumeRequest{
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

func (b *CommandBindings) RequestControl(
	request contracts.ControlRequest,
) (response contracts.ControlResponse, err error) {
	done := b.runtime.begin("RequestControl")
	defer func() { done(err) }()
	ctx, commands, err := b.bindingContext()
	if err != nil {
		return contracts.ControlResponse{}, err
	}
	result, err := commands.RequestControl(ctx, orchestrate.ControlRequest{
		RequestID: domainfoundation.AgentControlRequestID(request.RequestID), AgentID: domainfoundation.AgentID(request.AgentID),
		Kind: domainworkflow.AgentControlKind(request.Kind),
	})
	if err != nil {
		return contracts.ControlResponse{}, publicBindingError(err)
	}
	return contracts.ControlResponse{
		RequestID: result.Request.ID.String(), AgentID: result.Request.AgentID.String(),
		TargetExecutionID: result.Request.TargetExecutionID.String(), Kind: string(result.Request.Kind),
		Status: string(result.Request.Status), ExistingRequest: result.ExistingRequest,
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
	result, err := commands.EnqueueWork(ctx, orchestrate.QueueWorkRequest{
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
