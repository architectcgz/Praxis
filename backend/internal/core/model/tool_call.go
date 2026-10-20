package model

import (
	"bytes"
	"encoding/json"
	"praxis/internal/core/tool"
)

// ToolCall 是 Provider 与模型调用方共享的纯工具请求数据。
type ToolCall struct {
	ID        string
	Name      tool.ToolName
	Arguments json.RawMessage
}

// Snapshot 复制 JSON 参数，避免跨边界共享可变切片；身份由输入入口处理。
func (call ToolCall) Snapshot() ToolCall {
	call.Arguments = bytes.Clone(call.Arguments)
	return call
}
