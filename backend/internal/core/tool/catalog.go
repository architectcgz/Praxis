// Catalog 将工具可用性配置应用到注册目录。
package tool

import "maps"

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
