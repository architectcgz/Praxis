// Package toolconfig 加载工具可用性与 Bash 命令策略。
package toolconfig

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"strings"
	"sync"

	"praxis/internal/contracts"
	"praxis/internal/core/security"
	toolcontracts "praxis/internal/tools/contracts"
)

type ToolConfig struct {
	Enabled bool `json:"enabled"`
}

type Config struct {
	Tools map[contracts.ToolName]ToolConfig `json:"tools"`
	Bash  security.CommandPolicy            `json:"bash"`
}

// Load 在配置缺失时启用所有已注册工具，Bash 默认进入人工审核。
func Load(path string) (Config, error) {
	config := Config{Bash: security.CommandPolicy{DefaultAction: security.CommandAsk}}
	if path == "" {
		return config, nil
	}
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return config, nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("read tool config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return Config{}, fmt.Errorf("decode tool config: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		if err == nil {
			return Config{}, errors.New("tool config contains multiple JSON values")
		}
		return Config{}, fmt.Errorf("decode trailing tool config: %w", err)
	}
	for i := range config.Bash.Rules {
		config.Bash.Rules[i].ID = strings.TrimSpace(config.Bash.Rules[i].ID)
	}
	if err := config.Bash.Validate(); err != nil {
		return Config{}, err
	}
	return config, nil
}

func (c Config) ValidateRegistered(names []contracts.ToolName) error {
	registered := make(map[contracts.ToolName]bool, len(names))
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
	registry toolcontracts.ToolCatalog
}

func NewCatalog(config Config, registry toolcontracts.ToolCatalog) *Catalog {
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

func (c *Catalog) enabled(name contracts.ToolName) bool {
	tool, specified := c.config.Tools[name]
	return !specified || tool.Enabled
}

func (c *Catalog) Get(name contracts.ToolName) (toolcontracts.Tool, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if !c.enabled(name) {
		return nil, false
	}
	return c.registry.Get(name)
}

func (c *Catalog) List() []contracts.ToolDefinition {
	c.mu.RLock()
	defer c.mu.RUnlock()
	definitions := c.registry.List()
	result := make([]contracts.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		if c.enabled(definition.Name) {
			result = append(result, definition)
		}
	}
	return result
}

func (c *Catalog) DecideCommand(command string) security.CommandDecision {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.config.Bash.Decide(command)
}
