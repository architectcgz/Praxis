// Package tool 定义工具协议、注册目录与结果边界。
package tool

import (
	"encoding/json"
)

// ToolResult 是工具执行后交给本次执行实例和模型调用服务的结果。
// MaxInlineResultBytes 限制内联结果的 UTF-8 字节数，超出部分不能截断，完整结果转存临时文件。
const MaxInlineResultBytes = 50 * 1024

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
		Message string `json:"message,omitempty"`
	}{
		Error:   code,
		Message: message,
	})
	return ToolResult{
		Payload:    string(encoded),
		ErrorClass: code,
	}
}
