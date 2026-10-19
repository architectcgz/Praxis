// Registry 加载用户管理的 Agent 定义、指令与默认模型。
package agentregistry

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	agentmodel "praxis/internal/core/agent"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/fault"
	"praxis/internal/identity"
	"regexp"
	"slices"
	"strings"
	"sync"
)

var definitionIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,63}$`)

// ModelValidator 根据当前模型注册表校验模型引用。
type ModelValidator func(providerID, modelID string) error

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

// Registry 保存进程启动时加载并校验的 Agent 定义。
type Registry struct {
	mu          sync.RWMutex
	configs     map[identity.AgentDefinitionID]agentmodel.AgentConfig
	definitions map[identity.AgentDefinitionID]agentmodel.AgentDefinition
}

// Load 扫描根目录；一级目录名就是 definition_id。
func Load(root string, validateModel ModelValidator) (*Registry, error) {
	configs, definitions, err := loadDefinitions(root)
	if err != nil {
		return nil, &ConfigurationError{Path: root, Err: err}
	}
	registry := &Registry{
		configs:     configs,
		definitions: definitions,
	}
	if err := registry.ValidateModelReferences(validateModel); err != nil {
		return nil, &ConfigurationError{Path: root, Err: err}
	}
	return registry, nil
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

// ResolveModelReference 返回 Agent 定义绑定的默认模型引用。
func (r *Registry) ResolveModelReference(id identity.AgentDefinitionID) (agentmodel.ModelReference, error) {
	if r == nil {
		return agentmodel.ModelReference{}, errors.New("agent registry is not initialized")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.configs[id]
	if !ok || agent.Model == nil {
		return agentmodel.ModelReference{}, fmt.Errorf("agent definition %q has no configured model", id)
	}
	return *agent.Model, nil
}

// DefinitionsForModel 返回绑定指定模型的 Agent 定义 ID。
func (r *Registry) DefinitionsForModel(providerID, modelID string) []string {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	agents := make([]string, 0)
	for id, agent := range r.configs {
		if agent.Model != nil && agent.Model.ProviderID == providerID && agent.Model.ModelID == modelID {
			agents = append(agents, id.String())
		}
	}
	slices.Sort(agents)
	return agents
}

// ValidateModelReferences 校验所有已配置的 Agent 模型引用。
func (r *Registry) ValidateModelReferences(validateModel ModelValidator) error {
	if r == nil {
		return errors.New("agent registry is not initialized")
	}
	if validateModel == nil {
		return errors.New("agent model validator is required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for id, agent := range r.configs {
		if agent.Model == nil {
			continue
		}
		if err := validateModel(agent.Model.ProviderID, agent.Model.ModelID); err != nil {
			return fmt.Errorf("agents config: agent definition %q model: %w", id, err)
		}
	}
	return nil
}

func validateAgentConfig(config agentmodel.AgentConfig) (agentmodel.AgentConfig, error) {
	switch config.Profile {
	case agentmodel.ProfilePrimary, agentmodel.ProfileDelegate, agentmodel.ProfileAdvisor, agentmodel.ProfileCurator:
	default:
		return agentmodel.AgentConfig{}, fault.InvalidValue("agentDefinition.profile", "unknown agent profile")
	}
	result := agentmodel.AgentConfig{Profile: config.Profile}
	if config.Model != nil {
		model := agentmodel.ModelReference{
			ProviderID: config.Model.ProviderID,
			ModelID:    config.Model.ModelID,
		}
		if model.ProviderID == "" || model.ModelID == "" || model.ProviderID != strings.TrimSpace(model.ProviderID) || model.ModelID != strings.TrimSpace(model.ModelID) {
			return agentmodel.AgentConfig{}, errors.New("model providerId and modelId are required together")
		}
		result.Model = &model
	}
	return result, nil
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

func loadDefinitions(root string) (map[identity.AgentDefinitionID]agentmodel.AgentConfig, map[identity.AgentDefinitionID]agentmodel.AgentDefinition, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil, errors.New("agent definitions directory is required")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, fmt.Errorf("read agent definitions directory: %w", err)
	}
	configs := make(map[identity.AgentDefinitionID]agentmodel.AgentConfig, len(entries))
	definitions := make(map[identity.AgentDefinitionID]agentmodel.AgentDefinition, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, nil, fmt.Errorf("agent definition %q cannot be a symbolic link", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		id := identity.AgentDefinitionID(entry.Name())
		if !definitionIDPattern.MatchString(id.String()) {
			return nil, nil, fault.InvalidValue("agentDefinition.id", "definition id is invalid")
		}
		directory := filepath.Join(root, entry.Name())
		config, err := readAgentConfig(filepath.Join(directory, "agent.json"))
		if err != nil {
			return nil, nil, err
		}
		config, err = validateAgentConfig(config)
		if err != nil {
			return nil, nil, fmt.Errorf("agent definition %q: %w", id, err)
		}
		instructions, err := os.ReadFile(filepath.Join(directory, "AGENT.md"))
		if err != nil {
			return nil, nil, fmt.Errorf("read agent definition %q AGENT.md: %w", id, err)
		}
		content := string(instructions)
		if content == "" || content != strings.TrimSpace(content) {
			return nil, nil, fmt.Errorf("agent definition %q AGENT.md must be canonical and nonempty", id)
		}
		revision, err := definitionRevision(config, content)
		if err != nil {
			return nil, nil, fmt.Errorf("revision agent definition %q: %w", id, err)
		}
		definition := agentmodel.AgentDefinition{
			ID:           id,
			Profile:      config.Profile,
			Instructions: content,
			Revision:     revision,
		}
		configs[id] = config
		definitions[id] = definition
	}
	return configs, definitions, nil
}

func definitionRevision(config agentmodel.AgentConfig, instructions string) (string, error) {
	encoded, err := json.Marshal(struct {
		Config       agentmodel.AgentConfig `json:"config"`
		Instructions string                 `json:"instructions"`
	}{
		Config:       config,
		Instructions: instructions,
	})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(encoded)
	return fmt.Sprintf("sha256-%x", digest), nil
}

// ValidateModelConfiguration 确保所有 Agent 默认模型仍存在于候选配置中。
func (r *Registry) ValidateModelConfiguration(config modelconfig.Config) error {
	return r.ValidateModelReferences(func(providerID, modelID string) error {
		for _, provider := range config.Providers {
			if provider.ID != providerID {
				continue
			}
			for _, configured := range provider.Models {
				if configured.ID == modelID {
					return nil
				}
			}
		}
		return fmt.Errorf("model %q for provider %q is not configured", modelID, providerID)
	})
}

// ReplaceFrom 用已校验的磁盘快照替换 Agent 定义；已有会话的权限不随之提升。
func (r *Registry) ReplaceFrom(candidate *Registry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.configs = candidate.configs
	r.definitions = candidate.definitions
}
