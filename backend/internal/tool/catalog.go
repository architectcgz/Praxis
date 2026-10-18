// Catalog 将工具可用性配置与 Bash 命令策略应用到注册目录。
package tool

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"os"
	"praxis/internal/security"
	"strings"
	"sync"
)

type ToolConfig struct {
	Enabled bool `json:"enabled"`
}

type Config struct {
	Tools map[ToolName]ToolConfig `json:"tools"`
	Bash  security.CommandPolicy  `json:"bash"`
}

// Load 只读加载工具配置，缺失或非法文件直接返回错误。
func Load(path string) (Config, error) {
	var config Config
	encoded, err := os.ReadFile(path)
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
	if err := validateCommandPolicy(config.Bash); err != nil {
		return Config{}, err
	}
	return config, nil
}

// validateCommandPolicy 在配置文件输入边界检查规则，匹配值保留空格和换行。
func validateCommandPolicy(policy security.CommandPolicy) error {
	seen := make(map[string]bool, len(policy.Rules))
	for _, rule := range policy.Rules {
		if rule.ID == "" || rule.ID != strings.TrimSpace(rule.ID) || seen[rule.ID] {
			return fmt.Errorf("bash rule id %q must be unique and canonical", rule.ID)
		}
		seen[rule.ID] = true
		if !rule.Action.Valid() || strings.TrimSpace(rule.Match.Value) == "" {
			return fmt.Errorf("bash rule %q needs a valid action and non-empty match", rule.ID)
		}
		if rule.Match.Type != "exact" && rule.Match.Type != "prefix" {
			return fmt.Errorf("bash rule %q has an invalid match type", rule.ID)
		}
	}
	return nil
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
