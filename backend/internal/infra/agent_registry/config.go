// Package agentregistry owns the user-managed Agent definitions and their
// default model and security policy configuration.
package agentregistry

import (
	"praxis/internal/contracts"
	agentmodel "praxis/internal/core/agent"
	securitymodel "praxis/internal/core/security"
	workspacemodel "praxis/internal/core/workspace"

	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	appconfig "praxis/internal/modelconfig"
)

// ModelReference identifies one configured Provider and Model pair.
type ModelReference struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

// PolicyConfig is the user-editable security policy for one Agent.
type PolicyConfig struct {
	ApprovalMode       contracts.ApprovalMode    `json:"approvalMode"`
	SandboxMode        contracts.SandboxMode     `json:"sandboxMode"`
	AllowedTools       []contracts.ToolName      `json:"allowedTools"`
	AllowedExecutables []string                  `json:"allowedExecutables"`
	WorkspaceAccess    contracts.WorkspaceAccess `json:"workspaceAccess"`
}

// AgentConfig 保存一个 Agent 定义的角色、默认模型和安全策略。
type AgentConfig struct {
	Profile contracts.AgentProfile `json:"profile,omitempty"`
	Model   *ModelReference        `json:"model,omitempty"`
	Policy  PolicyConfig           `json:"policy"`
}

// ModelValidator checks a model reference against the current model registry.
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
	configs     map[contracts.AgentDefinitionID]AgentConfig
	definitions map[contracts.AgentDefinitionID]agentmodel.AgentDefinition
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
func (r *Registry) Definition(id contracts.AgentDefinitionID) (agentmodel.AgentDefinition, error) {
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
func (r *Registry) ResolveModelReference(id contracts.AgentDefinitionID) (ModelReference, error) {
	if r == nil {
		return ModelReference{}, errors.New("agent registry is not initialized")
	}
	if !id.Valid() {
		return ModelReference{}, fmt.Errorf("invalid agent definition %q", id)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.configs[id]
	if !ok || agent.Model == nil {
		return ModelReference{}, fmt.Errorf("agent definition %q has no configured model", id)
	}
	return *agent.Model, nil
}

// SecurityPolicy 为指定工作区生成 Agent 定义的初始安全策略。
func (r *Registry) SecurityPolicy(
	workspace workspacemodel.Workspace,
	id contracts.AgentDefinitionID,
) (securitymodel.AgentSecurityPolicy, error) {
	if r == nil {
		return securitymodel.AgentSecurityPolicy{}, errors.New("agent registry is not initialized")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.configs[id]
	if !ok {
		return securitymodel.AgentSecurityPolicy{}, fmt.Errorf("agent definition %q is not configured", id)
	}
	capabilities := securitymodel.CapabilityPolicy{
		AllowedTools:       slices.Clone(agent.Policy.AllowedTools),
		AllowedExecutables: slices.Clone(agent.Policy.AllowedExecutables),
	}
	switch agent.Policy.WorkspaceAccess {
	case contracts.WorkspaceAccessRead:
		capabilities.ReadScopes = []string{workspace.Path}
	case contracts.WorkspaceAccessReadWrite:
		capabilities.ReadScopes = []string{workspace.Path}
		capabilities.WriteScopes = []string{workspace.Path}
	}
	return securitymodel.NewAgentSecurityPolicy(
		1,
		capabilities,
		securitymodel.SandboxPolicy{Mode: agent.Policy.SandboxMode},
		securitymodel.ApprovalPolicy{Mode: agent.Policy.ApprovalMode},
	)
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

// ValidateModelReferences checks every configured Agent model reference.
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

func normalizeAgentConfig(config AgentConfig, fallback AgentConfig) (AgentConfig, error) {
	policy := config.Policy
	if policy.ApprovalMode == "" && policy.SandboxMode == "" && policy.WorkspaceAccess == "" &&
		len(policy.AllowedTools) == 0 {
		policy = fallback.Policy
	}
	result := AgentConfig{
		Profile: config.Profile,
		Policy: PolicyConfig{
			ApprovalMode:       policy.ApprovalMode,
			SandboxMode:        policy.SandboxMode,
			AllowedTools:       slices.Clone(policy.AllowedTools),
			AllowedExecutables: slices.Clone(policy.AllowedExecutables),
			WorkspaceAccess:    policy.WorkspaceAccess,
		},
	}
	if result.Profile == "" {
		result.Profile = fallback.Profile
	}
	if !result.Profile.Valid() {
		return AgentConfig{}, errors.New("profile is required and must be valid")
	}
	if config.Model != nil {
		model := ModelReference{
			ProviderID: strings.TrimSpace(config.Model.ProviderID),
			ModelID:    strings.TrimSpace(config.Model.ModelID),
		}
		if model.ProviderID == "" || model.ModelID == "" {
			return AgentConfig{}, errors.New("model providerId and modelId are required together")
		}
		result.Model = &model
	}
	template, err := contracts.NewDefaultGrantTemplate(
		result.Policy.AllowedTools,
		result.Policy.WorkspaceAccess,
	)
	if err != nil {
		return AgentConfig{}, fmt.Errorf("invalid policy: %w", err)
	}
	if !result.Policy.ApprovalMode.Valid() || !result.Policy.SandboxMode.Valid() {
		return AgentConfig{}, errors.New("invalid policy approvalMode or sandboxMode")
	}
	result.Policy.AllowedTools = template.AllowedTools
	result.Policy.WorkspaceAccess = template.WorkspaceAccess
	return result, nil
}

func readAgentConfig(path string) (AgentConfig, error) {
	encoded, err := os.ReadFile(path)
	if err != nil {
		return AgentConfig{}, fmt.Errorf("read agent config %q: %w", path, err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var config AgentConfig
	if err := decoder.Decode(&config); err != nil {
		return AgentConfig{}, fmt.Errorf("agent config %q: invalid JSON: %w", path, err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return AgentConfig{}, fmt.Errorf("agent config %q: invalid JSON: %w", path, err)
	}
	return config, nil
}

func writeAgentConfig(path string, config AgentConfig) error {
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agent config: %w", err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("create agent config %q: %w", path, err)
	}
	return nil
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

func loadDefinitions(root string) (map[contracts.AgentDefinitionID]AgentConfig, map[contracts.AgentDefinitionID]agentmodel.AgentDefinition, error) {
	if strings.TrimSpace(root) == "" {
		return nil, nil, errors.New("agent definitions directory is required")
	}
	if err := ensureDefaultDefinitions(root); err != nil {
		return nil, nil, err
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, fmt.Errorf("read agent definitions directory: %w", err)
	}
	defaults := defaultAgentConfigs()
	configs := make(map[contracts.AgentDefinitionID]AgentConfig, len(entries))
	definitions := make(map[contracts.AgentDefinitionID]agentmodel.AgentDefinition, len(entries))
	for _, entry := range entries {
		if entry.Type()&os.ModeSymlink != 0 {
			return nil, nil, fmt.Errorf("agent definition %q cannot be a symbolic link", entry.Name())
		}
		if !entry.IsDir() {
			continue
		}
		id := contracts.AgentDefinitionID(entry.Name())
		if !id.Valid() {
			return nil, nil, fmt.Errorf("invalid agent definition directory %q", entry.Name())
		}
		directory := filepath.Join(root, entry.Name())
		config, err := readAgentConfig(filepath.Join(directory, "agent.json"))
		if err != nil {
			return nil, nil, err
		}
		config, err = normalizeAgentConfig(config, defaults[id])
		if err != nil {
			return nil, nil, fmt.Errorf("agent definition %q: %w", id, err)
		}
		instructions, err := os.ReadFile(filepath.Join(directory, "AGENT.md"))
		if err != nil {
			return nil, nil, fmt.Errorf("read agent definition %q AGENT.md: %w", id, err)
		}
		content := strings.TrimSpace(string(instructions))
		if content == "" {
			return nil, nil, fmt.Errorf("agent definition %q has an empty AGENT.md", id)
		}
		revision, err := definitionRevision(config, content)
		if err != nil {
			return nil, nil, fmt.Errorf("revision agent definition %q: %w", id, err)
		}
		definition := agentmodel.AgentDefinition{
			ID:           id,
			Profile:      config.Profile,
			Instructions: content,
			AllowedTools: slices.Clone(config.Policy.AllowedTools),
			Revision:     revision,
		}
		if err := definition.Validate(); err != nil {
			return nil, nil, err
		}
		configs[id] = config
		definitions[id] = definition
	}
	return configs, definitions, nil
}

func ensureDefaultDefinitions(root string) error {
	if err := os.MkdirAll(root, 0o700); err != nil {
		return fmt.Errorf("create agent definitions directory: %w", err)
	}
	instructions := defaultAgentInstructions()
	for id, config := range defaultAgentConfigs() {
		directory := filepath.Join(root, id.String())
		if err := os.MkdirAll(directory, 0o700); err != nil {
			return fmt.Errorf("create agent definition %q directory: %w", id, err)
		}
		configPath := filepath.Join(directory, "agent.json")
		if _, err := os.Stat(configPath); errors.Is(err, os.ErrNotExist) {
			if err := writeAgentConfig(configPath, config); err != nil {
				return err
			}
		} else if err != nil {
			return fmt.Errorf("inspect agent definition %q config: %w", id, err)
		}
		instructionsPath := filepath.Join(directory, "AGENT.md")
		if _, err := os.Stat(instructionsPath); errors.Is(err, os.ErrNotExist) {
			if err := os.WriteFile(instructionsPath, []byte(instructions[id]), 0o600); err != nil {
				return fmt.Errorf("create agent definition %q AGENT.md: %w", id, err)
			}
		} else if err != nil {
			return fmt.Errorf("inspect agent definition %q AGENT.md: %w", id, err)
		}
	}
	return nil
}

func definitionRevision(config AgentConfig, instructions string) (string, error) {
	encoded, err := json.Marshal(struct {
		Config       AgentConfig `json:"config"`
		Instructions string      `json:"instructions"`
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

func defaultAgentConfigs() map[contracts.AgentDefinitionID]AgentConfig {
	readTools := []contracts.ToolName{
		contracts.ToolReadFile,
	}
	basePolicy := func(tools []contracts.ToolName, access contracts.WorkspaceAccess) PolicyConfig {
		return PolicyConfig{
			ApprovalMode:    contracts.ApprovalAlwaysAsk,
			SandboxMode:     contracts.SandboxReadOnly,
			AllowedTools:    slices.Clone(tools),
			WorkspaceAccess: access,
		}
	}
	return map[contracts.AgentDefinitionID]AgentConfig{
		agentmodel.DefinitionPrimary: {
			Profile: contracts.ProfilePrimary,
			Policy: PolicyConfig{
				ApprovalMode: contracts.ApprovalYolo,
				SandboxMode:  contracts.SandboxWorkspaceWrite,
				AllowedTools: []contracts.ToolName{
					contracts.ToolReadFile,
					contracts.ToolBash,
					contracts.ToolApplyPatch,
				},
				AllowedExecutables: []string{"bash"},
				WorkspaceAccess:    contracts.WorkspaceAccessReadWrite,
			},
		},
		agentmodel.DefinitionDelegate: {
			Profile: contracts.ProfileDelegate,
			Policy: basePolicy(
				readTools,
				contracts.WorkspaceAccessRead,
			),
		},
		agentmodel.DefinitionAdvisor: {
			Profile: contracts.ProfileAdvisor,
			Policy: basePolicy(
				readTools,
				contracts.WorkspaceAccessRead,
			),
		},
		agentmodel.DefinitionCurator: {
			Profile: contracts.ProfileCurator,
			Policy: basePolicy(
				[]contracts.ToolName{},
				contracts.WorkspaceAccessNone,
			),
		},
	}
}

func defaultAgentInstructions() map[contracts.AgentDefinitionID]string {
	return map[contracts.AgentDefinitionID]string{
		agentmodel.DefinitionPrimary:  "# Primary Agent\n\n你负责理解用户目标、推进任务，并在需要时委派独立工作。",
		agentmodel.DefinitionDelegate: "# Delegate Agent\n\n你负责完成被委派的具体任务，并返回可验证的结果。",
		agentmodel.DefinitionAdvisor:  "# Advisor Agent\n\n你负责分析问题、识别风险并给出明确建议，不直接改变工作区。",
		agentmodel.DefinitionCurator:  "# Curator Agent\n\n你负责整理已有信息并提交结构化结果。",
	}
}

// ValidateModelConfiguration 确保所有 Agent 默认模型仍存在于候选配置中。
func (r *Registry) ValidateModelConfiguration(config appconfig.Config) error {
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
