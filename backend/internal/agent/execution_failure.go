package agent

import (
	"strings"
)

// ExecutionFailureCode 是任务持久化结果的稳定失败原因。
type ExecutionFailureCode string

const (
	ExecutionFailureProviderUnavailable ExecutionFailureCode = "provider_unavailable"
	ExecutionFailureProvider            ExecutionFailureCode = "task_provider_error"
	ExecutionFailureTool                ExecutionFailureCode = "task_tool_error"
	ExecutionFailurePolicyBlocked       ExecutionFailureCode = "task_policy_blocked"
	ExecutionFailureApprovalRequired    ExecutionFailureCode = "task_approval_required"
	ExecutionFailureResourceLimit       ExecutionFailureCode = "task_resource_limit"
	ExecutionFailureStorage             ExecutionFailureCode = "task_storage_error"
	ExecutionFailureContract            ExecutionFailureCode = "task_contract_error"
	ExecutionFailureBusy                ExecutionFailureCode = "task_busy"
	ExecutionFailureInterrupted         ExecutionFailureCode = "task_interrupted"
	ExecutionFailureRequestCanceled     ExecutionFailureCode = "request_canceled"
	ExecutionFailureRuntimeCancelled    ExecutionFailureCode = "runtime_cancelled"
	ExecutionFailureRuntimeFailed       ExecutionFailureCode = "runtime_failed"
	ExecutionFailureRuntimeInvalid      ExecutionFailureCode = "runtime_invalid_outcome"
)

// MaxExecutionFailureMessageRunes 限制可展示的 Provider 失败详情，避免错误响应无限增长。
const MaxExecutionFailureMessageRunes = 4096

// ExecutionFailureMessage 只为 Provider 失败提取可展示详情，其他错误仍由前端按失败码展示。
func ExecutionFailureMessage(code ExecutionFailureCode, err error) string {
	if code != ExecutionFailureProvider || err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return ""
	}
	runes := []rune(message)
	if len(runes) > MaxExecutionFailureMessageRunes {
		return string(runes[:MaxExecutionFailureMessageRunes]) + "..."
	}
	return message
}
