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
	ToolListDir    = core.ToolListDir
	ToolSearchText = core.ToolSearchText
	ToolWriteFile  = core.ToolWriteFile
	ToolRunCommand = core.ToolRunCommand
	ToolBash       = core.ToolBash
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
	Input     json.RawMessage
	Arguments json.RawMessage
}

// Snapshot 复制原始参数，并补齐 Input 与 Arguments 的缺失值。
func (c ToolCall) Snapshot() ToolCall {
	if len(c.Input) == 0 && len(c.Arguments) > 0 {
		c.Input = c.Arguments
	}
	if len(c.Arguments) == 0 && len(c.Input) > 0 {
		c.Arguments = c.Input
	}
	c.Input = append(json.RawMessage(nil), c.Input...)
	c.Arguments = append(json.RawMessage(nil), c.Arguments...)
	return c
}

// NormalizedToolCall 是经过工具参数校验后的调用，尚未获得执行权限。
type NormalizedToolCall struct {
	Name                ToolName
	NormalizedArguments json.RawMessage
	Path                string
}

// Snapshot 复制规范化参数，防止跨边界修改。
func (c NormalizedToolCall) Snapshot() NormalizedToolCall {
	c.NormalizedArguments = append(json.RawMessage(nil), c.NormalizedArguments...)
	return c
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

// Snapshot 复制参数和权限范围，避免调用方修改执行器的输入。
func (c AuthorizedToolCall) Snapshot() AuthorizedToolCall {
	c.NormalizedArguments = append(json.RawMessage(nil), c.NormalizedArguments...)
	c.ReadScopes = append([]string(nil), c.ReadScopes...)
	c.WriteScopes = append([]string(nil), c.WriteScopes...)
	c.AllowedExecutables = append([]string(nil), c.AllowedExecutables...)
	return c
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
