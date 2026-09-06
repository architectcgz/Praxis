package command

import "errors"

// ErrorCode is a stable application command outcome exposed through bindings.
type ErrorCode string

const (
	ErrorAgentExecuting          ErrorCode = "agent_executing"
	ErrorAgentUnavailable        ErrorCode = "agent_unavailable"
	ErrorInvalidRequest          ErrorCode = "invalid_request"
	ErrorNotReady                ErrorCode = "orchestration_not_ready"
	ErrorProjectWorkspaceInvalid ErrorCode = "project_workspace_invalid"
	ErrorModelNotConfigured      ErrorCode = "model_not_configured"
)

// Error never contains persistence or provider details that could be exposed
// as a user-facing command outcome.
type Error struct {
	Code ErrorCode
}

func (e *Error) Error() string {
	return string(e.Code)
}

// NewError creates a stable command outcome.
func NewError(code ErrorCode) *Error {
	return &Error{Code: code}
}

// HasError reports whether err contains one stable command outcome.
func HasError(err error, code ErrorCode) bool {
	var command *Error
	return errors.As(err, &command) && command.Code == code
}
