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
	definition    func() tool.ToolDefinition
	call          func(context.Context, json.RawMessage, string) (tool.ToolResult, error)
	workspacePath string
}

func (t builtinTool) Definition() tool.ToolDefinition { return t.definition() }
func (t builtinTool) Call(ctx context.Context, args json.RawMessage) (tool.ToolResult, error) {
	return t.call(ctx, args, t.workspacePath)
}

func Bind(t tool.Tool, path string) tool.Tool {
	if builtin, ok := t.(builtinTool); ok {
		builtin.workspacePath = path
		return builtin
	}
	return t
}

func Prepare(name tool.ToolName, args json.RawMessage, workspacePath string) (json.RawMessage, error) {
	switch name {
	case tool.ToolBash:
		return bash.Prepare(args, workspacePath)
	case tool.ToolApplyPatch:
		return applypatch.Prepare(args, workspacePath)
	case tool.ToolReadFile:
		return readfile.Prepare(args, workspacePath)
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
		builtinTool{definition: readfile.Definition, call: readfile.Call},
		builtinTool{definition: bash.Definition, call: bash.Call},
		builtinTool{definition: applypatch.Definition, call: applypatch.Call},
	}
	for _, candidate := range append(builtins, extensions...) {
		if err := registry.Register(candidate); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
