// Registry 加载用户管理的 Agent 定义与指令。
package agentregistry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	agentmodel "praxis/internal/core/agent"
	"praxis/internal/errorcode"
	"praxis/internal/identity"
	"regexp"
	"sort"
	"strings"
	"sync"
)

var definitionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ConfigurationError 标识加载失败的 Agent 定义文件或目录。
type ConfigurationError struct {
	Path string
	Err  error
}

func (e *ConfigurationError) Error() string {
	if e == nil {
		return "agent configuration error"
	}
	return fmt.Sprintf("configuration %q: %v", e.Path, e.Err)
}

func (e *ConfigurationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// Registry 保存已加载并发布的 Agent 定义。
type Registry struct {
	mu          sync.RWMutex
	definitions map[identity.AgentDefinitionID]agentmodel.AgentDefinition
}

// Load 扫描根目录；一级目录名就是 definition_id。
func Load(root string) (*Registry, error) {
	definitions, err := loadDefinitions(root)
	if err != nil {
		return nil, &ConfigurationError{Path: root, Err: err}
	}
	return &Registry{definitions: definitions}, nil
}

// Definition 返回一个经过校验的 Agent 定义快照。
func (r *Registry) Definition(id identity.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
	if r == nil {
		return agentmodel.AgentDefinition{}, errors.New("agent registry is not initialized")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	definition, ok := r.definitions[id]
	if !ok {
		return agentmodel.AgentDefinition{}, fmt.Errorf("agent definition %q is not configured", id)
	}
	return definition.Snapshot(), nil
}

// List 按 ID 排序返回全部定义的独立快照。
func (r *Registry) List() []agentmodel.AgentDefinition {
	r.mu.RLock()
	defer r.mu.RUnlock()
	definitions := make([]agentmodel.AgentDefinition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		definitions = append(definitions, definition.Snapshot())
	}
	sort.Slice(definitions, func(i, j int) bool {
		return definitions[i].ID < definitions[j].ID
	})
	return definitions
}

func validateAgentConfig(config agentmodel.AgentConfig) error {
	switch config.Profile {
	case agentmodel.ProfilePrimary, agentmodel.ProfileDelegate, agentmodel.ProfileAdvisor, agentmodel.ProfileCurator:
		return nil
	default:
		return fmt.Errorf("agentDefinition.profile: unknown agent profile: %w", errorcode.ErrInvalidValue)
	}
}

func readAgentConfig(path string) (agentmodel.AgentConfig, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return agentmodel.AgentConfig{}, fmt.Errorf("read agent config %q: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var config agentmodel.AgentConfig
	if err := decoder.Decode(&config); err != nil {
		return agentmodel.AgentConfig{}, fmt.Errorf("agent config %q: invalid JSON: %w", path, err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return agentmodel.AgentConfig{}, fmt.Errorf("agent config %q: invalid JSON: %w", path, err)
	}
	return config, nil
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var extra any
	err := decoder.Decode(&extra)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values are not allowed")
	}
	return err
}

func loadDefinitions(root string) (map[identity.AgentDefinitionID]agentmodel.AgentDefinition, error) {
	if strings.TrimSpace(root) == "" {
		return nil, errors.New("agent definitions directory is required")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, fmt.Errorf("read agent definitions directory: %w", err)
	}
	definitions := make(map[identity.AgentDefinitionID]agentmodel.AgentDefinition, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("agent definition %q cannot be a symbolic link", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		id := identity.AgentDefinitionID(entry.Name())
		if !definitionIDPattern.MatchString(id.String()) {
			return nil, fmt.Errorf("agentDefinition.id: definition id is invalid: %w", errorcode.ErrInvalidValue)
		}
		directory := filepath.Join(root, entry.Name())
		config, err := readAgentConfig(filepath.Join(directory, "agent.json"))
		if err != nil {
			return nil, err
		}
		if err := validateAgentConfig(config); err != nil {
			return nil, fmt.Errorf("agent definition %q: %w", id, err)
		}
		instructions, err := os.ReadFile(filepath.Join(directory, "AGENT.md"))
		if err != nil {
			return nil, fmt.Errorf("read agent definition %q AGENT.md: %w", id, err)
		}
		content := string(instructions)
		if content == "" || content != strings.TrimSpace(content) {
			return nil, fmt.Errorf("agent definition %q AGENT.md must be canonical and nonempty", id)
		}
		definitions[id] = agentmodel.AgentDefinition{
			ID:           id,
			Profile:      config.Profile,
			Instructions: content,
		}
	}
	return definitions, nil
}

// ReplaceFrom 复制候选快照并整体发布，不与候选 Registry 共享可变 Map。
func (r *Registry) ReplaceFrom(candidate *Registry) {
	definitions := candidate.List()
	replacement := make(map[identity.AgentDefinitionID]agentmodel.AgentDefinition, len(definitions))
	for _, definition := range definitions {
		replacement[definition.ID] = definition
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.definitions = replacement
}
