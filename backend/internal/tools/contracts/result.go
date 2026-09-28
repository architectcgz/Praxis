// Package contracts 定义工具执行边界上共享的契约。
package contracts

import "encoding/json"

// ToolResult 是工具执行后返回给 runtime 和模型调用服务的结果。
type ToolResult struct {
	Payload    string
	ErrorClass string
	SideEffect bool
	Truncated  bool
}

// NewToolSuccess 创建成功的工具结果。
// payload 由具体 tool 负责生成，contracts 只统一结果 envelope。
func NewToolSuccess(payload string, truncated bool) ToolResult {
	return ToolResult{
		Payload:   payload,
		Truncated: truncated,
	}
}

// NewToolError 创建统一格式的工具错误结果。
// 错误详情只作为工具结果返回给模型，不作为 Go error 向上抛出。
func NewToolError(code, message string) ToolResult {
	encoded, _ := json.Marshal(struct {
		Error   string `json:"error"`
		Message string `json:"message"`
	}{
		Error:   code,
		Message: message,
	})
	return ToolResult{
		Payload:    string(encoded),
		ErrorClass: code,
	}
}
