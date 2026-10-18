package bootstrap

import (
	"context"
	"praxis/internal/core/tool"
	"praxis/internal/infra/tools/applypatch"
	"praxis/internal/infra/tools/bash"
	"praxis/internal/infra/tools/readfile"
)

type builtinTool struct {
	definition func() tool.ToolDefinition
	normalize  func(tool.ToolCall, string) (tool.NormalizedToolCall, error)
	execute    func(context.Context, tool.ExecutionCall) (tool.ToolResult, error)
}

func (t builtinTool) Definition() tool.ToolDefinition { return t.definition() }
func (t builtinTool) Normalize(call tool.ToolCall, path string) (tool.NormalizedToolCall, error) {
	return t.normalize(call, path)
}
func (t builtinTool) Execute(ctx context.Context, call tool.ExecutionCall) (tool.ToolResult, error) {
	return t.execute(ctx, call)
}

func buildToolRegistry(extensions []tool.Tool) (*tool.ToolRegistry, error) {
	registry := tool.NewToolRegistry()
	builtins := []tool.Tool{
		builtinTool{
			definition: readfile.Definition,
			normalize:  readfile.Normalize,
			execute:    readfile.Execute,
		},
		builtinTool{
			definition: bash.Definition,
			normalize:  bash.Normalize,
			execute:    bash.Execute,
		},
		builtinTool{
			definition: applypatch.Definition,
			normalize:  applypatch.Normalize,
			execute:    applypatch.Execute,
		},
	}
	for _, candidate := range append(builtins, extensions...) {
		if err := registry.Register(candidate); err != nil {
			return nil, err
		}
	}
	return registry, nil
}
