package contracts

import (
	"context"
	"encoding/json"

	core "praxis/internal/contracts"
)

// ToolName 是工具系统对外使用的工具名称类型。
type ToolName = core.ToolName

const (
	ToolReadFile   = core.ToolReadFile
	ToolBash       = core.ToolBash
	ToolApplyPatch = core.ToolApplyPatch
)

// Tool 是工具包对外暴露的行为契约；审核决策由工具执行层负责。
type Tool interface {
	Definition() core.ToolDefinition
	// Normalize 在工具参数输入边界一次完成规范化与校验，成功时返回完整的 canonical 参数和路径。
	Normalize(core.ToolCall, string) (NormalizedToolCall, error)
	// Execute 消费已规范化或已在磁盘恢复入口校验的参数，并检查当前权限及实际访问对象。
	Execute(context.Context, ExecutionCall) (ToolResult, error)
}

// ToolCatalog 提供当前可用的工具目录和按名称查找能力。
type ToolCatalog interface {
	// List 返回当前目录中的工具定义。
	List() []core.ToolDefinition
	// Get 按名称查找工具；不存在或未启用时返回 false。
	Get(ToolName) (Tool, bool)
}

// PreparedToolCall 携带一次参数准备的结果；失败也必须进入持久化结算。
type PreparedToolCall struct {
	Call       core.ToolCall
	Normalized NormalizedToolCall
	Err        error
}

// NormalizedToolCall 是经过工具参数校验后的调用，尚未经过执行准入。
type NormalizedToolCall struct {
	Name                ToolName
	NormalizedArguments json.RawMessage
	Path                string
}

// ExecutionCall 是通过准入后交给工具的规范化输入。
// 执行器负责参数解析、当前访问范围及其与授权对象的一致性，不重复清洗字段。
type ExecutionCall struct {
	Name                ToolName
	NormalizedArguments json.RawMessage
	Path                string
	WorkspacePath       string
}
