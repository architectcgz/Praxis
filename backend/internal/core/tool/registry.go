package tool

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"
)

var toolNamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_]{0,63}$`)

type ToolConfig struct {
	Enabled bool `json:"enabled"`
}

type Config struct {
	Tools map[ToolName]ToolConfig `json:"tools"`
}

// Registration 保存工具与注册时授予的执行范围。
type Registration struct {
	Tool      Tool
	RunScoped bool
}

// ToolRegistry 保存已注册的工具，装配完成后由模型请求与执行器共同读取。
type ToolRegistry struct {
	tools map[ToolName]Registration
}

// NewToolRegistry 创建空注册表，具体工具由装配入口注册。
func NewToolRegistry() *ToolRegistry {
	return &ToolRegistry{tools: make(map[ToolName]Registration)}
}

// Register 注册扩展工具；名称或 Schema 无效、名称已被占用时返回错误。
func (r *ToolRegistry) Register(candidate Tool) error {
	return r.register(candidate, false)
}

// RegisterRunTool 注册框架为本次 Run 绑定的工具，其权限由绑定的操作校验。
func (r *ToolRegistry) RegisterRunTool(candidate Tool) error {
	return r.register(candidate, true)
}

func (r *ToolRegistry) register(candidate Tool, runScoped bool) error {
	if candidate == nil {
		return errors.New("tool registry and tool are required")
	}
	definition := candidate.Definition()
	if !runScoped && definition.Name == listToolsName {
		return fmt.Errorf("tool name %q is reserved for discovery", definition.Name)
	}
	if !toolNamePattern.MatchString(string(definition.Name)) || !json.Valid(definition.InputSchema) || definition.Description == "" {
		return errors.New("tool definition is invalid")
	}
	if _, exists := r.tools[definition.Name]; exists {
		return fmt.Errorf("tool %q is already registered", definition.Name)
	}
	r.tools[definition.Name] = Registration{Tool: candidate, RunScoped: runScoped}
	return nil
}

func (r *ToolRegistry) Get(name ToolName) (Registration, bool) {
	registration, ok := r.tools[name]
	return registration, ok
}

// List 返回全部已注册工具的定义副本，按名称排序。
func (r *ToolRegistry) List() []ToolDefinition {
	definitions := make([]ToolDefinition, 0, len(r.tools))
	for _, registration := range r.tools {
		definitions = append(definitions, registration.Tool.Definition())
	}
	slices.SortFunc(definitions, func(left, right ToolDefinition) int {
		return strings.Compare(string(left.Name), string(right.Name))
	})
	return definitions
}

// Enabled 冻结当前会话启用的工具，未配置的已注册工具默认启用。
func (r *ToolRegistry) Enabled(config Config) *ToolRegistry {
	enabled := NewToolRegistry()
	for name, registration := range r.tools {
		if setting, configured := config.Tools[name]; !configured || setting.Enabled {
			enabled.tools[name] = registration
		}
	}
	return enabled
}

func (r *ToolRegistry) Clone() *ToolRegistry {
	return &ToolRegistry{tools: maps.Clone(r.tools)}
}

// Restrict 按委派请求选取工具。作用于本次 Run 的框架工具由创建流程分别绑定。
func (r *ToolRegistry) Restrict(names []ToolName) (*ToolRegistry, error) {
	allowed := NewToolRegistry()
	for _, name := range names {
		registration, ok := r.Get(name)
		if !ok || registration.RunScoped {
			return nil, fmt.Errorf("tool %s is outside the parent authorization", name)
		}
		allowed.tools[name] = registration
	}
	return allowed, nil
}
