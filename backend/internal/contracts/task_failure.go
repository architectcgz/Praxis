package contracts

import (
	"strings"
)

// TaskFailureCode 是持久化回合和 Wails 共用的稳定失败原因。
type TaskFailureCode string

const (
	TaskFailureProviderUnavailable TaskFailureCode = "provider_unavailable"
	TaskFailureProvider            TaskFailureCode = "task_provider_error"
	TaskFailureTool                TaskFailureCode = "task_tool_error"
	TaskFailurePolicyBlocked       TaskFailureCode = "task_policy_blocked"
	TaskFailureApprovalRequired    TaskFailureCode = "task_approval_required"
	TaskFailureResourceLimit       TaskFailureCode = "task_resource_limit"
	TaskFailureStorage             TaskFailureCode = "task_storage_error"
	TaskFailureContract            TaskFailureCode = "task_contract_error"
	TaskFailureBusy                TaskFailureCode = "task_busy"
	TaskFailureInterrupted         TaskFailureCode = "task_interrupted"
	TaskFailureRequestCanceled     TaskFailureCode = "request_canceled"
	TaskFailureRuntimeCancelled    TaskFailureCode = "runtime_cancelled"
	TaskFailureRuntimeFailed       TaskFailureCode = "runtime_failed"
	TaskFailureRuntimeInvalid      TaskFailureCode = "runtime_invalid_outcome"
)

// MaxTaskFailureMessageRunes 限制可展示的 Provider 失败详情，避免错误响应无限增长。
const MaxTaskFailureMessageRunes = 4096

// TaskFailureMessage 只为 Provider 失败提取可展示详情，其他错误仍由前端按失败码展示。
func TaskFailureMessage(code TaskFailureCode, err error) string {
	if code != TaskFailureProvider || err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return ""
	}
	runes := []rune(message)
	if len(runes) > MaxTaskFailureMessageRunes {
		return string(runes[:MaxTaskFailureMessageRunes]) + "..."
	}
	return message
}

func (c TaskFailureCode) Valid() bool {
	switch c {
	case "", TaskFailureProviderUnavailable, TaskFailureProvider,
		TaskFailureTool, TaskFailurePolicyBlocked,
		TaskFailureApprovalRequired, TaskFailureResourceLimit,
		TaskFailureStorage, TaskFailureContract, TaskFailureBusy,
		TaskFailureInterrupted, TaskFailureRequestCanceled,
		TaskFailureRuntimeCancelled, TaskFailureRuntimeFailed,
		TaskFailureRuntimeInvalid:
		return true
	default:
		return false
	}
}
