package runtime

import (
	"praxis/internal/contracts"
	toolcontracts "praxis/internal/tools/contracts"

	"context"
)

// ToolInvocationMetadata 描述 runtime 发起一次 Tool 调用时的执行归属和运行边界。
type ToolInvocationMetadata struct {
	ExecutionID   contracts.AgentExecutionID
	SessionID     contracts.SessionID
	AgentID       contracts.AgentID
	WorkspacePath string
	Execution     contracts.RuntimeExecutionSnapshot
}

// ToolCatalog 提供完整的工具目录和按名称查找能力；权限筛选由上层完成。
type ToolCatalog interface {
	List() []toolcontracts.ToolDefinition
	Get(toolcontracts.ToolName) (toolcontracts.Tool, bool)
}

// ToolCallHandler 接收 runtime 产生的工具调用，并负责完成持久化调用流程。
type ToolCallHandler interface {
	Invoke(context.Context, toolcontracts.ToolCall, ToolInvocationMetadata) (toolcontracts.ToolResult, error)
}
