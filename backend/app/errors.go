package app

import (
	"context"
	"errors"

	"praxis/internal/application/agent_runtime"
	applicationquery "praxis/internal/application/query"
	commandprotocol "praxis/internal/command"
	"praxis/internal/contracts"
	domainfoundation "praxis/internal/domain/foundation"
)

func publicBindingError(err error) error {
	if err == nil {
		return nil
	}
	var command *commandprotocol.Error
	if errors.As(err, &command) {
		return publicCommandError(command)
	}
	var query *applicationquery.InvalidQueryError
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

func publicCommandError(command *commandprotocol.Error) error {
	if command == nil {
		return bindingError(contracts.ErrorCodeInternal)
	}
	switch command.Code {
	case commandprotocol.ErrorAgentExecuting:
		return bindingError(contracts.ErrorCodeAgentExecuting)
	case commandprotocol.ErrorAgentUnavailable:
		return bindingError(contracts.ErrorCodeAgentUnavailable)
	case commandprotocol.ErrorInvalidRequest:
		return bindingError(contracts.ErrorCodeInvalidRequest)
	case commandprotocol.ErrorNotReady:
		return bindingError(contracts.ErrorCodeNotReady)
	case commandprotocol.ErrorProjectWorkspaceInvalid:
		return bindingError(contracts.ErrorCodeProjectWorkspaceInvalid)
	case commandprotocol.ErrorModelNotConfigured:
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
