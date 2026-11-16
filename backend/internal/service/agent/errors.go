package agent

import (
	sessionentry "praxis/internal/core/entry"
	"strings"
)

// RunFailureMessage 只为 Provider 失败提取可展示详情，其他错误仍由前端按失败码展示。
func RunFailureMessage(code sessionentry.RunFailureCode, err error) string {
	if code != sessionentry.RunFailureProvider || err == nil {
		return ""
	}
	message := strings.TrimSpace(err.Error())
	if message == "" {
		return ""
	}
	runes := []rune(message)
	if len(runes) > sessionentry.MaxRunFailureMessageRunes {
		return string(runes[:sessionentry.MaxRunFailureMessageRunes]) + "..."
	}
	return message
}
