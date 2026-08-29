package app

import (
	"context"
	"errors"

	"praxis/internal/agentruntime"
	"praxis/internal/contracts"
	"praxis/internal/core/domain"
	"praxis/internal/core/orchestrate"
)

func publicBindingError(err error) error {
	if err == nil {
		return nil
	}
	var command *orchestrate.CommandError
	if errors.As(err, &command) {
		return publicCommandError(command)
	}
	if executionError, ok := publicExecutionError(err); ok {
		return executionError
	}
	switch {
	case errors.Is(err, context.Canceled):
		return bindingError(contracts.ErrorCodeRequestCanceled)
	case errors.Is(err, context.DeadlineExceeded):
		return bindingError(contracts.ErrorCodeRequestTimeout)
	case errors.Is(err, domain.ErrInvalidValue):
		return bindingError(contracts.ErrorCodeValidation)
	case errors.Is(err, domain.ErrInvalidTransition):
		return bindingError(contracts.ErrorCodeInvalidTransition)
	case errors.Is(err, domain.ErrLeaseConflict):
		return bindingError(contracts.ErrorCodeWorkspaceConflict)
	case errors.Is(err, domain.ErrAlreadySettled):
		return bindingError(contracts.ErrorCodeAlreadySettled)
	case errors.Is(err, domain.ErrWorkQueueEmpty):
		return bindingError(contracts.ErrorCodeWorkQueueEmpty)
	case errors.Is(err, domain.ErrWorkItemActive):
		return bindingError(contracts.ErrorCodeWorkItemActive)
	case errors.Is(err, domain.ErrAlreadyDelivered):
		return bindingError(contracts.ErrorCodeAlreadyDelivered)
	case errors.Is(err, domain.ErrRequestNotFound):
		return bindingError(contracts.ErrorCodeRequestNotFound)
	case errors.Is(err, domain.ErrRequestConflict):
		return bindingError(contracts.ErrorCodeRequestConflict)
	case errors.Is(err, domain.ErrNotFound):
		return bindingError(contracts.ErrorCodeNotFound)
	}
	return bindingError(contracts.ErrorCodeInternal)
}

func bindingError(code contracts.ErrorCode) error {
	return errors.New(code.String())
}

func publicCommandError(command *orchestrate.CommandError) error {
	if command == nil {
		return bindingError(contracts.ErrorCodeInternal)
	}
	switch command.Code {
	case orchestrate.CommandErrorAgentExecuting:
		return bindingError(contracts.ErrorCodeAgentExecuting)
	case orchestrate.CommandErrorAgentUnavailable:
		return bindingError(contracts.ErrorCodeAgentUnavailable)
	case orchestrate.CommandErrorInvalidRequest:
		return bindingError(contracts.ErrorCodeInvalidRequest)
	case orchestrate.CommandErrorNotReady:
		return bindingError(contracts.ErrorCodeNotReady)
	case orchestrate.CommandErrorProjectWorkspaceInvalid:
		return bindingError(contracts.ErrorCodeProjectWorkspaceInvalid)
	default:
		return bindingError(contracts.ErrorCodeInternal)
	}
}

func publicExecutionError(err error) (error, bool) {
	switch {
	case agentruntime.IsCode(err, agentruntime.ErrorBusy):
		return bindingError(contracts.ErrorCodeExecutionBusy), true
	case agentruntime.IsCode(err, agentruntime.ErrorContract):
		return bindingError(contracts.ErrorCodeExecutionContract), true
	case agentruntime.IsCode(err, agentruntime.ErrorPolicyBlocked):
		return bindingError(contracts.ErrorCodeExecutionPolicyBlocked), true
	case agentruntime.IsCode(err, agentruntime.ErrorApprovalRequired):
		return bindingError(contracts.ErrorCodeExecutionApproval), true
	case agentruntime.IsCode(err, agentruntime.ErrorStorage):
		return bindingError(contracts.ErrorCodeExecutionStorage), true
	case agentruntime.IsCode(err, agentruntime.ErrorProvider):
		return bindingError(contracts.ErrorCodeExecutionProvider), true
	case agentruntime.IsCode(err, agentruntime.ErrorTool):
		return bindingError(contracts.ErrorCodeExecutionTool), true
	case agentruntime.IsCode(err, agentruntime.ErrorResourceLimit):
		return bindingError(contracts.ErrorCodeExecutionResourceLimit), true
	case agentruntime.IsCode(err, agentruntime.ErrorClosed):
		return bindingError(contracts.ErrorCodeExecutionClosed), true
	case agentruntime.IsCode(err, agentruntime.ErrorInterrupted):
		return bindingError(contracts.ErrorCodeExecutionInterrupted), true
	default:
		return nil, false
	}
}
