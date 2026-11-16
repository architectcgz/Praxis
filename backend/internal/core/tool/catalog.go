// Catalog 将工具可用性配置应用到注册目录。
package tool

import (
	"fmt"
	"maps"
	"slices"
)

type ToolConfig struct {
	Enabled bool `json:"enabled"`
}

type Config struct {
	Tools map[ToolName]ToolConfig `json:"tools"`
}

func (c Config) clone() Config {
	c.Tools = maps.Clone(c.Tools)
	return c
}

// Catalog 将同一份已校验配置用于工具暴露、准入和启动前检查。
// 目录在会话内只读，显式 reload 构造新目录，不修改旧实例。
type Catalog struct {
	config   Config
	registry ToolCatalog
}

func NewCatalog(config Config, registry ToolCatalog) *Catalog {
	return &Catalog{config: config.clone(), registry: registry}
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
	c := &restrictedCatalog{registry: parent, names: make(map[ToolName]bool), definitions: make([]ToolDefinition, 0, len(names))}
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
func (c *restrictedCatalog) List() []ToolDefinition { return slices.Clone(c.definitions) }
