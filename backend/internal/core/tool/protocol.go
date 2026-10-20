package tool

import (
	"bytes"
	"encoding/json"
)

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
