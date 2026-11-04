package run

// RunFailureCode 是任务持久化结果的稳定失败原因。
type RunFailureCode string

const (
	RunFailureProviderUnavailable RunFailureCode = "provider_unavailable"
	RunFailureProvider            RunFailureCode = "execution_provider_error"
	RunFailureTool                RunFailureCode = "execution_tool_error"
	RunFailurePolicyBlocked       RunFailureCode = "execution_policy_blocked"
	RunFailureApprovalRequired    RunFailureCode = "execution_approval_required"
	RunFailureResourceLimit       RunFailureCode = "execution_resource_limit"
	RunFailureStorage             RunFailureCode = "execution_storage_error"
	RunFailureContract            RunFailureCode = "execution_contract_error"
	RunFailureBusy                RunFailureCode = "execution_busy"
	RunFailureInterrupted         RunFailureCode = "execution_interrupted"
	RunFailureRequestCanceled     RunFailureCode = "request_canceled"
	RunFailureCancelled           RunFailureCode = "execution_cancelled"
	RunFailureInternal            RunFailureCode = "execution_failed"
	RunFailureInvalidOutcome      RunFailureCode = "execution_invalid_outcome"
)

// MaxRunFailureMessageRunes 限制持久化失败详情的字符数。
const MaxRunFailureMessageRunes = 4096
