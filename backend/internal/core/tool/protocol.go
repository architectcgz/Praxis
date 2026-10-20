package tool

import "encoding/json"

// ToolDefinition 是 Provider 中立的模型可见工具定义。
type ToolDefinition struct {
	Name        ToolName
	Description string
	InputSchema json.RawMessage
}
