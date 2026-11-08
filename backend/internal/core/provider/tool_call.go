package provider

import "praxis/internal/core/tool"

// ToolCall 是 Provider 与模型调用方共享的纯工具请求数据。
type ToolCall struct {
	ID   string
	Name tool.ToolName
	// Arguments 保留模型返回的参数原文，合法性由工具执行入口判断。
	Arguments string
}
