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
	ToolSearchText = core.ToolSearchText
	ToolWriteFile  = core.ToolWriteFile
	ToolRunCommand = core.ToolRunCommand
	ToolBash       = core.ToolBash
	ToolApplyPatch = core.ToolApplyPatch
)

// Tool 是工具包对外暴露的行为契约；权限判断不属于该接口。
type Tool interface {
	Definition() core.ToolDefinition
	Normalize(core.ToolCall) (NormalizedToolCall, error)
	Execute(context.Context, AuthorizedToolCall) (ToolResult, error)
}

// ToolCatalog 提供完整的工具目录和按名称查找能力；权限筛选由调用方完成。
type ToolCatalog interface {
	// List 返回全部已注册工具的定义，不做权限筛选。
	List() []core.ToolDefinition
	// Get 按名称查找工具；未注册时返回 false。
	Get(ToolName) (Tool, bool)
}

// NormalizedToolCall 是经过工具参数校验后的调用，尚未获得执行权限。
type NormalizedToolCall struct {
	Name                ToolName
	NormalizedArguments json.RawMessage
	Path                string
}

// AuthorizedToolCall 是权限方批准后交给执行器的输入。
// 执行器仍需检查工具自身的路径和参数约束。
type AuthorizedToolCall struct {
	Name                ToolName
	NormalizedArguments json.RawMessage
	Path                string
	ReadScopes          []string
	WriteScopes         []string
	AllowedExecutables  []string
}
