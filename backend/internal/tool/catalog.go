// Catalog 将工具可用性配置应用到注册目录。
package tool

import (
	"fmt"
	"slices"
	"strings"
)

type ToolConfig struct {
	Enabled bool `json:"enabled"`
}

type Config struct {
	Tools map[ToolName]ToolConfig `json:"tools"`
}

// Catalog 将同一份已校验配置用于工具暴露、准入和启动前检查。
// 目录在会话内只读，显式 reload 构造新目录，不修改旧实例。
type Catalog struct {
	config   Config
	registry ToolCatalog
}

func NewCatalog(config Config, registry ToolCatalog) *Catalog {
	return &Catalog{config: config, registry: registry}
}

func (c *Catalog) enabled(name ToolName) bool {
	tool, specified := c.config.Tools[name]
	return !specified || tool.Enabled
}

func (c *Catalog) Get(name ToolName) (Tool, bool) {
	if !c.enabled(name) {
		return nil, false
	}
	return c.registry.Get(name)
}

func (c *Catalog) List() []ToolDefinition {
	definitions := c.registry.List()
	result := make([]ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if c.enabled(definition.Name) {
			result = append(result, definition)
		}
	}
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
