package agent

import (
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/fault"
	"strings"
)

const (
	ExecutionBusy          fault.Code = "execution.busy"
	ExecutionContract      fault.Code = "execution.contract_error"
	ExecutionPolicyBlocked fault.Code = "execution.policy_blocked"
	ExecutionApproval      fault.Code = "execution.approval_required"
	ExecutionStorage       fault.Code = "execution.storage_error"
	ExecutionProvider      fault.Code = "execution.provider_error"
	ExecutionTool          fault.Code = "execution.tool_error"
)

// ExecutionFailureMessage 只为 Provider 失败提取可展示详情，其他错误仍由前端按失败码展示。
func ExecutionFailureMessage(code agentexecution.ExecutionFailureCode, err error) string {
	if code != agentexecution.ExecutionFailureProvider || err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return ""
	}
	runes := []rune(message)
	if len(runes) > agentexecution.MaxExecutionFailureMessageRunes {
		return string(runes[:agentexecution.MaxExecutionFailureMessageRunes]) + "..."
	}
	return message
}
