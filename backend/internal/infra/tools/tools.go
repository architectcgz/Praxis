package tools

import (
	"context"
	"encoding/json"
	"praxis/internal/core/tool"
	"praxis/internal/infra/tools/applypatch"
	"praxis/internal/infra/tools/bash"
	"praxis/internal/infra/tools/readfile"
)

type builtinTool struct {
	definition      func() tool.ToolDefinition
	prepare         func(json.RawMessage, string) (json.RawMessage, error)
	call            func(context.Context, json.RawMessage, string) (tool.ToolResult, error)
	approvalCommand func(json.RawMessage) (string, error)
	workspacePath   string
}

func (t builtinTool) Definition() tool.ToolDefinition { return t.definition() }
func (t builtinTool) Prepare(args json.RawMessage, workspacePath string) (json.RawMessage, error) {
	return t.prepare(args, workspacePath)
}
func (t builtinTool) Call(ctx context.Context, args json.RawMessage) (tool.ToolResult, error) {
	return t.call(ctx, args, t.workspacePath)
}

// ApprovalCommand 提取已准备参数中的待审批命令；非命令工具返回空字符串。
func (t builtinTool) ApprovalCommand(args json.RawMessage) (string, error) {
	if t.approvalCommand == nil {
		return "", nil
	}
	return t.approvalCommand(args)
}

func Bind(t tool.Tool, path string) tool.Tool {
	if builtin, ok := t.(builtinTool); ok {
		builtin.workspacePath = path
		return builtin
	}
	return t
}

func NewRegistry(extensions []tool.Tool) (*tool.ToolRegistry, error) {
	registry := tool.NewToolRegistry()
	builtins := []tool.Tool{
		builtinTool{
			definition: readfile.Definition,
			prepare:    readfile.Prepare,
			call:       readfile.Call,
		},
		builtinTool{
			definition:      bash.Definition,
			prepare:         bash.Prepare,
			call:            bash.Call,
			approvalCommand: bash.ApprovalCommand,
		},
		builtinTool{
			definition: applypatch.Definition,
			prepare:    applypatch.Prepare,
			call:       applypatch.Call,
		},
	}
	for _, candidate := range append(builtins, extensions...) {
		if err := registry.Register(candidate); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
