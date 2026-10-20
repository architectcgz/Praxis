package tool

import (
	"context"
	"encoding/json"
)

// Tool 接收已准备好的参数，资源访问约束由具体工具在副作用边界检查。
type Tool interface {
	Definition() ToolDefinition
	Call(context.Context, json.RawMessage) (ToolResult, error)
}

// ToolCatalog 提供当前启用的工具定义和实现。
type ToolCatalog interface {
	List() []ToolDefinition
	Get(ToolName) (Tool, bool)
}
