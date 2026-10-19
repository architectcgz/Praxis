package agent

// ExecutionFailureCode 是任务持久化结果的稳定失败原因。
type ExecutionFailureCode string

const (
	ExecutionFailureProviderUnavailable ExecutionFailureCode = "provider_unavailable"
	ExecutionFailureProvider            ExecutionFailureCode = "execution_provider_error"
	ExecutionFailureTool                ExecutionFailureCode = "execution_tool_error"
	ExecutionFailurePolicyBlocked       ExecutionFailureCode = "execution_policy_blocked"
	ExecutionFailureApprovalRequired    ExecutionFailureCode = "execution_approval_required"
	ExecutionFailureResourceLimit       ExecutionFailureCode = "execution_resource_limit"
	ExecutionFailureStorage             ExecutionFailureCode = "execution_storage_error"
	ExecutionFailureContract            ExecutionFailureCode = "execution_contract_error"
	ExecutionFailureBusy                ExecutionFailureCode = "execution_busy"
	ExecutionFailureInterrupted         ExecutionFailureCode = "execution_interrupted"
	ExecutionFailureRequestCanceled     ExecutionFailureCode = "request_canceled"
	ExecutionFailureCancelled           ExecutionFailureCode = "execution_cancelled"
	ExecutionFailureInternal            ExecutionFailureCode = "execution_failed"
	ExecutionFailureInvalidOutcome      ExecutionFailureCode = "execution_invalid_outcome"
)

// MaxExecutionFailureMessageRunes 限制持久化失败详情的字符数。
const MaxExecutionFailureMessageRunes = 4096
