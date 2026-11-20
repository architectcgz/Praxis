package entry

import (
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// Key 复用工具契约的调用身份。
type Key = tool.CallKey

// ToolCall 是工具调用及执行归属的内存表示，调用信息随 Assistant Entry 持久化。
type ToolCall struct {
	Key
	SessionID identity.SessionID
	AgentID   identity.AgentID
	TurnID    identity.TurnID
	Name      tool.ToolName
	Arguments string
}
