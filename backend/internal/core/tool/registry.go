package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"slices"
)

var toolNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)

// ToolRegistry 保存工具实现，并对外提供完整的已注册工具目录。
type ToolRegistry struct {
	tools map[ToolName]Tool
}

// NewToolRegistry 创建空注册表，具体工具由装配入口注册。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[ToolName]Tool)}
}

// Register 注册扩展工具；名称或 Schema 无效、名称已被占用时返回错误。
func (r *ToolRegistry) Register(tool Tool) error {
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
func (r *ToolRegistry) Get(name ToolName) (Tool, bool) {
	if r == nil {
		return nil, false
	}
	tool, ok := r.tools[name]
	return tool, ok
}

// List 返回所有已注册工具的定义副本，按名称排序且不做权限筛选。
func (r *ToolRegistry) List() []ToolDefinition {
	if r == nil {
		return nil
	}
	definitions := make([]ToolDefinition, 0, len(r.tools))
	for _, tool := range r.tools {
		definitions = append(definitions, tool.Definition())
	}
	slices.SortFunc(definitions, func(left, right ToolDefinition) int {
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
