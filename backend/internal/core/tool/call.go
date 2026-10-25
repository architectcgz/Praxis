package tool

import (
	"context"
	"encoding/json"
)

// Tool 负责参数准备和实际调用，资源访问约束由具体工具在副作用边界检查。
type Tool interface {
	// Definition 返回独立副本，InputSchema 不与工具内部共享可变数据。
	Definition() ToolDefinition
	// Prepare 在审批前校验并规范化参数；参数无效时返回错误，不产生外部副作用或修改共享实例。
	Prepare(args json.RawMessage, workspacePath string) (json.RawMessage, error)
	// Call 使用已准备并授权的参数执行工具。
	Call(context.Context, json.RawMessage) (ToolResult, error)
}

// ToolCatalog 提供当前启用的工具定义和实现。
type ToolCatalog interface {
	List() []ToolDefinition
	Get(ToolName) (Tool, bool)
}
