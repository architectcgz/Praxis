package contracts

import (
	"bytes"
	"encoding/json"
	"strings"
)

// ToolCall 是 Provider 与模型调用方共享的纯工具请求数据。
type ToolCall struct {
	ID        string
	Name      ToolName
	Arguments json.RawMessage
}

// Snapshot 规范化调用 ID 并复制 JSON 参数，避免跨边界共享可变切片。
func (call ToolCall) Snapshot() ToolCall {
	call.ID = strings.TrimSpace(call.ID)
	call.Arguments = bytes.Clone(call.Arguments)
	return call
}

// ToolDefinition 是 Provider 中立的模型可见工具定义。
type ToolDefinition struct {
	Name        ToolName
	Description string
	InputSchema json.RawMessage
}

// Snapshot 复制输入 Schema，避免调用方修改注册表数据。
func (definition ToolDefinition) Snapshot() ToolDefinition {
	definition.InputSchema = bytes.Clone(definition.InputSchema)
	return definition
}
