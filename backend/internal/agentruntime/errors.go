package agentruntime

import "errors"

// ErrorCode is a stable runtime failure category for orchestration and UI mapping.
type ErrorCode string

const (
	ErrorBusy             ErrorCode = "runtime_busy"
	ErrorContract         ErrorCode = "contract_error"
	ErrorPolicyBlocked    ErrorCode = "policy_blocked"
	ErrorApprovalRequired ErrorCode = "approval_required"
	ErrorStorage          ErrorCode = "storage_error"
	ErrorProvider         ErrorCode = "provider_error"
	ErrorTool             ErrorCode = "tool_error"
	ErrorInterrupted      ErrorCode = "runtime_interrupted"
	ErrorResourceLimit    ErrorCode = "resource_limit"
	ErrorClosed           ErrorCode = "runtime_closed"
)

// RuntimeError is a low-sensitivity runtime error with a stable category.
type RuntimeError struct {
	Code    ErrorCode
	Message string
	Cause   error
}

// Error implements error.
func (e *RuntimeError) Error() string {
	if e.Message == "" {
		return string(e.Code)
	}
	return e.Message
}

// Unwrap exposes the cause to internal callers without changing outward messages.
func (e *RuntimeError) Unwrap() error { return e.Cause }

// IsCode reports whether err contains the supplied runtime error code.
func IsCode(err error, code ErrorCode) bool {
	var runtimeErr *RuntimeError
	return errors.As(err, &runtimeErr) && runtimeErr.Code == code
}
