package message

import (
	"encoding/json"
	"praxis/internal/core/tool"
	"praxis/internal/core/toolexecution"
	"praxis/internal/identity"
	"time"
)

// ToolCall 是从 Assistant 消息按需读取的调用，不另行持久化。
type ToolCall struct {
	toolexecution.Key
	SessionID identity.SessionID
	AgentID   identity.AgentID
	RequestID string
	TurnID    identity.TurnID
	Name      tool.ToolName
	Arguments json.RawMessage
	CreatedAt time.Time
}
