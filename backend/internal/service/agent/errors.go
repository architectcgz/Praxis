package agent

import (
	agentrun "praxis/internal/core/agent/run"
	"strings"
)

// RunFailureMessage 只为 Provider 失败提取可展示详情，其他错误仍由前端按失败码展示。
func RunFailureMessage(code agentrun.RunFailureCode, err error) string {
	if code != agentrun.RunFailureProvider || err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return ""
	}
	runes := []rune(message)
	if len(runes) > agentrun.MaxRunFailureMessageRunes {
		return string(runes[:agentrun.MaxRunFailureMessageRunes]) + "..."
	}
	return message
}
