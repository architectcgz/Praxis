package tool

import (
	"encoding/json"
	"fmt"
	"praxis/internal/identity"
)

// ToolDefinition 是 Provider 中立的模型可见工具定义。
type ToolDefinition struct {
	Name        ToolName
	Description string
	InputSchema json.RawMessage
}

// CallKey 用执行身份和 Provider 调用 ID 定位同一次工具调用。
type CallKey struct {
	ExecutionID identity.AgentExecutionID
	CallID      string
}

func (k CallKey) String() string {
	return fmt.Sprintf("%s:%d:%s", k.ExecutionID, len(k.CallID), k.CallID)
}
