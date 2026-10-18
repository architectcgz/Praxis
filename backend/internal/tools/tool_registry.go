package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	coretool "praxis/internal/core/tool"
	applypatch "praxis/internal/tools/apply_patch"
	bash "praxis/internal/tools/bash"
	toolcontracts "praxis/internal/tools/contracts"
	readfile "praxis/internal/tools/read_file"
	"regexp"
	"slices"
)

var toolNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)

type builtinTool struct {
	definition func() coretool.ToolDefinition
	normalize  func(coretool.ToolCall, string) (toolcontracts.NormalizedToolCall, error)
	execute    func(context.Context, toolcontracts.ExecutionCall) (toolcontracts.ToolResult, error)
}

func (t builtinTool) Definition() coretool.ToolDefinition { return t.definition() }

func (t builtinTool) Normalize(
	call coretool.ToolCall,
	workspacePath string,
) (toolcontracts.NormalizedToolCall, error) {
	return t.normalize(call, workspacePath)
}

func (t builtinTool) Execute(
	ctx context.Context,
	call toolcontracts.ExecutionCall,
) (toolcontracts.ToolResult, error) {
	return t.execute(ctx, call)
}

// ToolRegistry 保存工具实现，并对外提供完整的已注册工具目录。
type ToolRegistry struct {
	tools map[coretool.ToolName]toolcontracts.Tool
}

// NewToolRegistry 注册内置工具；可通过 Register 添加扩展工具。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: map[coretool.ToolName]toolcontracts.Tool{
		coretool.ToolReadFile: builtinTool{
			definition: readfile.Definition,
			normalize:  readfile.Normalize,
			execute:    readfile.Execute,
		},
		coretool.ToolBash: builtinTool{
			definition: bash.Definition,
			normalize:  bash.Normalize,
			execute:    bash.Execute,
		},
		coretool.ToolApplyPatch: builtinTool{
			definition: applypatch.Definition,
			normalize:  applypatch.Normalize,
			execute:    applypatch.Execute,
		},
	}}
}

// Register 注册扩展工具；名称或 Schema 无效、名称已被占用时返回错误。
func (r *ToolRegistry) Register(tool toolcontracts.Tool) error {
	if r == nil || tool == nil {
		return errors.New("tool registry and tool are required")
	}
	definition := tool.Definition()
	if !toolNamePattern.MatchString(string(definition.Name)) || !json.Valid(definition.InputSchema) || definition.Description == "" {
		return errors.New("tool definition is invalid")
	}
	if _, exists := r.tools[definition.Name]; exists {
		return fmt.Errorf("tool %q is already registered", definition.Name)
	}
	r.tools[definition.Name] = tool
	return nil
}

// Get 根据名称查找工具；注册表不负责决定工具是否可用。
func (r *ToolRegistry) Get(name coretool.ToolName) (toolcontracts.Tool, bool) {
	if r == nil {
		return nil, false
	}
	tool, ok := r.tools[name]
	return tool, ok
}

// List 返回所有已注册工具的定义副本，按名称排序且不做权限筛选。
func (r *ToolRegistry) List() []coretool.ToolDefinition {
	if r == nil {
		return nil
	}
	definitions := make([]coretool.ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		definitions = append(definitions, tool.Definition().Snapshot())
	}
	slices.SortFunc(definitions, func(left, right coretool.ToolDefinition) int {
		if left.Name < right.Name {
			return -1
		}
		if left.Name > right.Name {
			return 1
		}
		return 0
	})
	return definitions
}
