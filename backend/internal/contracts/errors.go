package contracts

// ErrorCode is the stable error vocabulary exposed by Wails bindings.
// Binding errors carry the code as their error string and never expose internal causes.
type ErrorCode string

const (
	ErrorCodeInvalidRequest          ErrorCode = "invalid_request"
	ErrorCodeConfiguration           ErrorCode = "configuration_error"
	ErrorCodeValidation              ErrorCode = "validation_error"
	ErrorCodeInvalidTransition       ErrorCode = "invalid_transition"
	ErrorCodeNotReady                ErrorCode = "orchestration_not_ready"
	ErrorCodeBindingUnavailable      ErrorCode = "binding_unavailable"
	ErrorCodeNotFound                ErrorCode = "not_found"
	ErrorCodeAgentExecuting          ErrorCode = "agent_executing"
	ErrorCodeAgentUnavailable        ErrorCode = "agent_unavailable"
	ErrorCodeRequestNotFound         ErrorCode = "request_not_found"
	ErrorCodeRequestConflict         ErrorCode = "request_conflict"
	ErrorCodeWorkspaceConflict       ErrorCode = "workspace_conflict"
	ErrorCodeAlreadySettled          ErrorCode = "already_settled"
	ErrorCodeWorkQueueEmpty          ErrorCode = "work_queue_empty"
	ErrorCodeWorkItemActive          ErrorCode = "work_item_active"
	ErrorCodeAlreadyDelivered        ErrorCode = "already_delivered"
	ErrorCodeProjectWorkspaceInvalid ErrorCode = "project_workspace_invalid"
	ErrorCodeRequestCanceled         ErrorCode = "request_canceled"
	ErrorCodeRequestTimeout          ErrorCode = "request_timeout"
	ErrorCodeExecutionContract       ErrorCode = "execution_contract_error"
	ErrorCodeExecutionPolicyBlocked  ErrorCode = "execution_policy_blocked"
	ErrorCodeExecutionApproval       ErrorCode = "execution_approval_required"
	ErrorCodeExecutionStorage        ErrorCode = "execution_storage_error"
	ErrorCodeExecutionProvider       ErrorCode = "execution_provider_error"
	ErrorCodeExecutionTool           ErrorCode = "execution_tool_error"
	ErrorCodeExecutionResourceLimit  ErrorCode = "execution_resource_limit"
	ErrorCodeExecutionBusy           ErrorCode = "execution_busy"
	ErrorCodeExecutionClosed         ErrorCode = "execution_closed"
	ErrorCodeExecutionInterrupted    ErrorCode = "execution_interrupted"
	ErrorCodeInternal                ErrorCode = "internal_error"
)

func (c ErrorCode) String() string { return string(c) }
