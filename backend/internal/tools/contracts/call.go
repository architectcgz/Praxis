package contracts

import (
	"context"
	"encoding/json"
	"strings"

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
	Definition() ToolDefinition
	Normalize(ToolCall) (NormalizedToolCall, error)
	Execute(context.Context, AuthorizedToolCall) (ToolResult, error)
}

// ToolCall 表示与 provider 无关的工具请求。
type ToolCall struct {
	ID        string
	Name      ToolName
	Arguments json.RawMessage
}

// Snapshot 复制原始参数并规范化 provider call ID，避免跨边界修改输入。
func (c ToolCall) Snapshot() ToolCall {
	c.ID = strings.TrimSpace(c.ID)
	c.Arguments = append(json.RawMessage(nil), c.Arguments...)
	return c
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

// ToolDefinition 描述已注册工具的模型契约；是否暴露由调用方决定。
type ToolDefinition struct {
	Name        ToolName
	Description string
	InputSchema json.RawMessage
}

// Snapshot 复制输入 schema，避免外部修改工具定义。
func (d ToolDefinition) Snapshot() ToolDefinition {
	d.InputSchema = append(json.RawMessage(nil), d.InputSchema...)
	return d
}
