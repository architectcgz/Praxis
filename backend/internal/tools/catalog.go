package tools

import (
	"fmt"
	"slices"
	"strings"
)

// Catalog 同时为模型暴露和执行准入提供装配期解析出的启用工具。
type Catalog struct {
	enabled map[ToolName]Tool
}

// NewCatalog 接管启用集合；发布后不再修改。
func NewCatalog(enabled map[ToolName]Tool) *Catalog { return &Catalog{enabled: enabled} }

func (c *Catalog) Get(name ToolName) (Tool, bool) {
	value, ok := c.enabled[name]
	return value, ok
}

func (c *Catalog) List() []ToolDefinition {
	result := make([]ToolDefinition, 0, len(c.enabled))
	for _, value := range c.enabled {
		result = append(result, value.Definition())
	}
	slices.SortFunc(result, func(a, b ToolDefinition) int { return strings.Compare(string(a.Name), string(b.Name)) })
	return result
}

// Restrict 冻结本次委派的工具子集，同一目录用于模型暴露和执行鉴权。
type restrictedCatalog struct {
	registry    ToolCatalog
	names       map[ToolName]bool
	definitions []ToolDefinition
}

func Restrict(parent ToolCatalog, names []ToolName) (ToolCatalog, error) {
	c := &restrictedCatalog{
		registry:    parent,
		names:       make(map[ToolName]bool),
		definitions: make([]ToolDefinition, 0, len(names)),
	}
	for _, name := range names {
		if _, ok := parent.Get(name); !ok {
			return nil, fmt.Errorf("tool %s is outside the parent authorization", name)
		}
		c.names[name] = true
	}
	for _, d := range parent.List() {
		if c.names[d.Name] {
			c.definitions = append(c.definitions, d)
		}
	}
	return c, nil
}
func (c *restrictedCatalog) Get(name ToolName) (Tool, bool) {
	if !c.names[name] {
		return nil, false
	}
	return c.registry.Get(name)
}
func (c *restrictedCatalog) List() []ToolDefinition { return c.definitions }

// ComposeCatalog 将额外工具（如运行期协作工具）与基础业务工具目录组合为单一只读目录。
func ComposeCatalog(base ToolCatalog, extras ...Tool) (ToolCatalog, error) {
	baseDefinitions := base.List()
	definitions := make([]ToolDefinition, len(baseDefinitions), len(baseDefinitions)+len(extras))
	copy(definitions, baseDefinitions)
	composed := &composedCatalog{
		base:        base,
		extras:      make(map[ToolName]Tool, len(extras)),
		definitions: definitions,
	}
	for _, value := range extras {
		definition := value.Definition()
		if _, exists := base.Get(definition.Name); exists {
			return nil, fmt.Errorf("tool name %s is reserved for native collaboration", definition.Name)
		}
		if _, duplicated := composed.extras[definition.Name]; duplicated {
			return nil, fmt.Errorf("duplicate native tool name %s", definition.Name)
		}
		composed.extras[definition.Name] = value
		composed.definitions = append(composed.definitions, definition)
	}
	slices.SortFunc(composed.definitions, func(a, b ToolDefinition) int {
		return strings.Compare(string(a.Name), string(b.Name))
	})
	return composed, nil
}

type composedCatalog struct {
	base        ToolCatalog
	extras      map[ToolName]Tool
	definitions []ToolDefinition
}

func (c *composedCatalog) Get(name ToolName) (Tool, bool) {
	if value, ok := c.extras[name]; ok {
		return value, true
	}
	return c.base.Get(name)
}

func (c *composedCatalog) List() []ToolDefinition { return c.definitions }
