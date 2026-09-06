package app

import (
	"context"
	"errors"

	"praxis/internal/application/agent_runtime"
	"praxis/internal/contracts"
	corecommand "praxis/internal/core/command"
	domainfoundation "praxis/internal/core/domain/foundation"

	"praxis/internal/core/projection"
)

func publicBindingError(err error) error {
	if err == nil {
		return nil
	}
	var command *corecommand.Error
	if errors.As(err, &command) {
		return publicCommandError(command)
	}
	var query *projection.InvalidQueryError
	if errors.As(err, &query) {
		return bindingError(contracts.ErrorCodeInvalidRequest)
	}
	if executionError, ok := publicExecutionError(err); ok {
		return executionError
	}
	switch {
	case errors.Is(err, context.Canceled):
		return bindingError(contracts.ErrorCodeRequestCanceled)
	case errors.Is(err, context.DeadlineExceeded):
		return bindingError(contracts.ErrorCodeRequestTimeout)
	case errors.Is(err, domainfoundation.ErrInvalidValue):
		return bindingError(contracts.ErrorCodeValidation)
	case errors.Is(err, domainfoundation.ErrInvalidTransition):
		return bindingError(contracts.ErrorCodeInvalidTransition)
	case errors.Is(err, domainfoundation.ErrLeaseConflict):
		return bindingError(contracts.ErrorCodeWorkspaceConflict)
	case errors.Is(err, domainfoundation.ErrAlreadySettled):
		return bindingError(contracts.ErrorCodeAlreadySettled)
	case errors.Is(err, domainfoundation.ErrWorkQueueEmpty):
		return bindingError(contracts.ErrorCodeWorkQueueEmpty)
	case errors.Is(err, domainfoundation.ErrWorkItemActive):
		return bindingError(contracts.ErrorCodeWorkItemActive)
	case errors.Is(err, domainfoundation.ErrAlreadyDelivered):
		return bindingError(contracts.ErrorCodeAlreadyDelivered)
	case errors.Is(err, domainfoundation.ErrRequestNotFound):
		return bindingError(contracts.ErrorCodeRequestNotFound)
	case errors.Is(err, domainfoundation.ErrRequestConflict):
		return bindingError(contracts.ErrorCodeRequestConflict)
	case errors.Is(err, domainfoundation.ErrNotFound):
		return bindingError(contracts.ErrorCodeNotFound)
	}
	return bindingError(contracts.ErrorCodeInternal)
}

func bindingError(code contracts.ErrorCode) error {
	return errors.New(code.String())
}

func publicCommandError(command *corecommand.Error) error {
	if command == nil {
		return bindingError(contracts.ErrorCodeInternal)
	}
	switch command.Code {
	case corecommand.ErrorAgentExecuting:
		return bindingError(contracts.ErrorCodeAgentExecuting)
	case corecommand.ErrorAgentUnavailable:
		return bindingError(contracts.ErrorCodeAgentUnavailable)
	case corecommand.ErrorInvalidRequest:
		return bindingError(contracts.ErrorCodeInvalidRequest)
	case corecommand.ErrorNotReady:
		return bindingError(contracts.ErrorCodeNotReady)
	case corecommand.ErrorProjectWorkspaceInvalid:
		return bindingError(contracts.ErrorCodeProjectWorkspaceInvalid)
	case corecommand.ErrorModelNotConfigured:
		return bindingError(contracts.ErrorCodeModelNotConfigured)
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
