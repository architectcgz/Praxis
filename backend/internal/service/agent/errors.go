package agent

import (
	agentexecution "praxis/internal/core/agent/execution"
	"strings"
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
