package app

import (
	"praxis/internal/contracts"
	"praxis/internal/core/domain"
	"praxis/internal/core/orchestrate"
)

func (a *App) SendInput(request contracts.SendInputRequest) (response contracts.SendInputResponse, err error) {
	done := a.beginBinding("SendInput")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.SendInputResponse{}, err
	}
	runtime, err := runtimeSnapshot(request.SandboxMode, request.ApprovalMode, request.Revision)
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	result, err := service.SendInput(ctx, orchestrate.SendInputRequest{
		AgentID: domain.AgentID(request.AgentID), RequestID: domain.RequestID(request.RequestID),
		Content: request.Content, ModelID: request.ModelID, Reasoning: request.Reasoning,
		RuntimeSnapshot: runtime,
	})
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	return contracts.SendInputResponse{
		ExecutionID: result.Execution.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (a *App) Resume(request contracts.ResumeRequest) (response contracts.SendInputResponse, err error) {
	done := a.beginBinding("Resume")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.SendInputResponse{}, err
	}
	runtime, err := runtimeSnapshot(request.SandboxMode, request.ApprovalMode, request.Revision)
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	result, err := service.Resume(ctx, orchestrate.ResumeRequest{
		AgentID: domain.AgentID(request.AgentID), RequestID: domain.RequestID(request.RequestID),
		Content: request.Content, RuntimeSnapshot: runtime,
	})
	if err != nil {
		return contracts.SendInputResponse{}, publicBindingError(err)
	}
	return contracts.SendInputResponse{
		ExecutionID: result.Execution.ID.String(), ExistingRequest: result.ExistingRequest,
		ActivationError: result.ActivationError,
	}, nil
}

func (a *App) RequestControl(request contracts.ControlRequest) (response contracts.ControlResponse, err error) {
	done := a.beginBinding("RequestControl")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.ControlResponse{}, err
	}
	result, err := service.RequestControl(ctx, orchestrate.ControlRequest{
		ID: domain.AgentControlRequestID(request.ID), AgentID: domain.AgentID(request.AgentID),
		Kind: domain.AgentControlKind(request.Kind),
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

func (a *App) QueueWork(request contracts.QueueWorkRequest) (response contracts.QueueWorkResponse, err error) {
	done := a.beginBinding("QueueWork")
	defer func() { done(err) }()
	ctx, service, err := a.bindingContext()
	if err != nil {
		return contracts.QueueWorkResponse{}, err
	}
	runtime, err := runtimeSnapshot(request.SandboxMode, request.ApprovalMode, request.Revision)
	if err != nil {
		return contracts.QueueWorkResponse{}, publicBindingError(err)
	}
	result, err := service.EnqueueWork(ctx, orchestrate.QueueWorkRequest{
		ID: domain.WorkItemID(request.ID), AgentID: domain.AgentID(request.AgentID),
		Prompt: request.Prompt, RuntimeSnapshot: runtime,
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
