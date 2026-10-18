// Catalog 将工具可用性配置与 Bash 命令策略应用到注册目录。
package tool

import (
	"fmt"
	"maps"
	"praxis/internal/security"
	"sync"
)

type ToolConfig struct {
	Enabled bool `json:"enabled"`
}

type Config struct {
	Tools map[ToolName]ToolConfig `json:"tools"`
	Bash  security.CommandPolicy  `json:"bash"`
}

func (c Config) ValidateRegistered(names []ToolName) error {
	registered := make(map[ToolName]bool, len(names))
	for _, name := range names {
		registered[name] = true
	}
	for name := range c.Tools {
		if !registered[name] {
			return fmt.Errorf("tool configuration refers to unregistered tool %q", name)
		}
	}
	return nil
}

func (c Config) clone() Config {
	c.Tools = maps.Clone(c.Tools)
	c.Bash = c.Bash.Clone()
	return c
}

// Catalog 将同一份已校验配置用于工具暴露、准入和启动前检查。
// ReplaceFrom 由组合根在与执行准入共用的事务中调用。
type Catalog struct {
	mu       sync.RWMutex
	config   Config
	registry ToolCatalog
}

func NewCatalog(config Config, registry ToolCatalog) *Catalog {
	return &Catalog{config: config.clone(), registry: registry}
}

func (c *Catalog) ReplaceFrom(candidate *Catalog) {
	candidate.mu.RLock()
	config := candidate.config.clone()
	candidate.mu.RUnlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.config = config
}

func (c *Catalog) enabled(name ToolName) bool {
	tool, specified := c.config.Tools[name]
	return !specified || tool.Enabled
}

func (c *Catalog) Get(name ToolName) (Tool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.enabled(name) {
		return nil, false
	}
	return c.registry.Get(name)
}

func (c *Catalog) List() []ToolDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	definitions := c.registry.List()
	result := make([]ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if c.enabled(definition.Name) {
			result = append(result, definition)
		}
	}
	return result
}

func (c *Catalog) DecideCommand(command string) security.ApprovalDecision {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config.Bash.Decide(command)
}
