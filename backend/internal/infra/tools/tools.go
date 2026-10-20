package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"praxis/internal/core/tool"
	"praxis/internal/infra/tools/applypatch"
	"praxis/internal/infra/tools/bash"
	"praxis/internal/infra/tools/readfile"
	"praxis/internal/utils/jsonutil"
)

type builtinTool struct {
	definition      func() tool.ToolDefinition
	prepare         func(json.RawMessage, string) (json.RawMessage, error)
	call            func(context.Context, json.RawMessage, string) (tool.ToolResult, error)
	approvalCommand func(json.RawMessage) (string, error)
	workspacePath   string
}

func (t builtinTool) Definition() tool.ToolDefinition { return t.definition() }
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

// Prepare 使用注册工具自带的参数准备函数，扩展工具默认校验 JSON。
func Prepare(t tool.Tool, args json.RawMessage, workspacePath string) (json.RawMessage, error) {
	if builtin, ok := t.(builtinTool); ok {
		return builtin.prepare(args, workspacePath)
	}
	if !json.Valid(args) {
		return nil, errors.New("tool arguments must be valid JSON")
	}
	if err := jsonutil.RejectDuplicateFields(args); err != nil {
		return nil, err
	}
	return bytes.Clone(args), nil
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
