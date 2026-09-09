// Package agentregistry owns the user-managed Agent definitions and their
// default model and security policy configuration.
package agentregistry

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	domainmodel "praxis/internal/domain/model"
	domainsecurity "praxis/internal/domain/security"
	domainworkspace "praxis/internal/domain/workspace"
)

// ModelReference identifies one configured Provider and Model pair.
type ModelReference struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

// PolicyConfig is the user-editable security policy for one Agent.
type PolicyConfig struct {
	ApprovalMode      domainsecurity.ApprovalMode       `json:"approvalMode"`
	SandboxMode       domainsecurity.SandboxMode        `json:"sandboxMode"`
	AllowedTools      []domainsecurity.ToolName         `json:"allowedTools"`
	WorkspaceAccess   domainsecurity.WorkspaceAccess    `json:"workspaceAccess"`
	ResultPermissions []domainsecurity.ResultPermission `json:"resultPermissions"`
}

// AgentConfig combines one Agent's default model and security policy.
type AgentConfig struct {
	Model  *ModelReference `json:"model,omitempty"`
	Policy PolicyConfig    `json:"policy"`
}

// FileConfig is the persisted agents.json document.
type FileConfig struct {
	Agents map[domainsecurity.AgentProfile]AgentConfig `json:"agents"`
}

// ModelValidator checks a model reference against the current model registry.
type ModelValidator func(providerID, modelID string) error

// ConfigurationError identifies the agents.json file that failed to load.
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

// Registry holds one validated agents.json document for the lifetime of the
// application process. Agent policy revisions remain durable per-Agent state.
type Registry struct {
	config FileConfig
}

// Load reads and validates one agents.json document.
func Load(path string, validateModel ModelValidator) (*Registry, error) {
	config, err := readFileConfig(path)
	if err != nil {
		return nil, &ConfigurationError{Path: path, Err: err}
	}
	config, err = Validate(config)
	if err != nil {
		return nil, &ConfigurationError{Path: path, Err: err}
	}
	registry := &Registry{config: config}
	if err := registry.ValidateModelReferences(validateModel); err != nil {
		return nil, &ConfigurationError{Path: path, Err: err}
	}
	return registry, nil
}

// ResolveModel returns the model assigned to an Agent profile.
func (r *Registry) ResolveModel(profile domainsecurity.AgentProfile) (domainmodel.ModelSelection, error) {
	if r == nil {
		return domainmodel.ModelSelection{}, errors.New("agent registry is not initialized")
	}
	if !profile.Valid() {
		return domainmodel.ModelSelection{}, fmt.Errorf("unknown agent profile %q", profile)
	}
	agent, ok := r.config.Agents[profile]
	if !ok || agent.Model == nil {
		return domainmodel.ModelSelection{}, fmt.Errorf("agent %q has no configured model", profile)
	}
	return domainmodel.NewModelSelection(agent.Model.ProviderID, agent.Model.ModelID, "")
}

// SecurityPolicy materializes the configured policy for one workspace and Agent.
func (r *Registry) SecurityPolicy(
	workspace domainworkspace.Workspace,
	profile domainsecurity.AgentProfile,
) (domainsecurity.AgentSecurityPolicy, error) {
	if r == nil {
		return domainsecurity.AgentSecurityPolicy{}, errors.New("agent registry is not initialized")
	}
	agent, ok := r.config.Agents[profile]
	if !ok {
		return domainsecurity.AgentSecurityPolicy{}, fmt.Errorf("agent %q is not configured", profile)
	}
	capabilities := domainsecurity.CapabilityPolicy{
		AllowedTools: append([]domainsecurity.ToolName(nil), agent.Policy.AllowedTools...),
	}
	switch agent.Policy.WorkspaceAccess {
	case domainsecurity.WorkspaceAccessRead:
		capabilities.ReadScopes = []string{workspace.Path}
	case domainsecurity.WorkspaceAccessReadWrite:
		capabilities.ReadScopes = []string{workspace.Path}
		capabilities.WriteScopes = []string{workspace.Path}
	}
	return domainsecurity.NewAgentSecurityPolicy(
		1,
		capabilities,
		domainsecurity.SandboxPolicy{Mode: agent.Policy.SandboxMode},
		domainsecurity.ApprovalPolicy{Mode: agent.Policy.ApprovalMode},
	)
}

// AgentsForModel returns the Agent profiles assigned to a model.
func (r *Registry) AgentsForModel(providerID, modelID string) []string {
	if r == nil {
		return nil
	}
	agents := make([]string, 0)
	for profile, agent := range r.config.Agents {
		if agent.Model != nil && agent.Model.ProviderID == providerID && agent.Model.ModelID == modelID {
			agents = append(agents, string(profile))
		}
	}
	sort.Strings(agents)
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
	for profile, agent := range r.config.Agents {
		if agent.Model == nil {
			continue
		}
		if err := validateModel(agent.Model.ProviderID, agent.Model.ModelID); err != nil {
			return fmt.Errorf("agents config: agent %q model: %w", profile, err)
		}
	}
	return nil
}

// Validate normalizes the document and fills omitted Agent definitions with
// conservative defaults.
func Validate(config FileConfig) (FileConfig, error) {
	resolved := defaultAgentConfigs()
	for profile, agent := range config.Agents {
		if !profile.Valid() {
			return FileConfig{}, fmt.Errorf("agents config: unknown agent %q", profile)
		}
		normalized, err := normalizeAgentConfig(agent, resolved[profile].Policy)
		if err != nil {
			return FileConfig{}, fmt.Errorf("agents config: agent %q: %w", profile, err)
		}
		resolved[profile] = normalized
	}
	return FileConfig{Agents: resolved}, nil
}

func normalizeAgentConfig(config AgentConfig, fallback PolicyConfig) (AgentConfig, error) {
	policy := config.Policy
	if policy.ApprovalMode == "" && policy.SandboxMode == "" && policy.WorkspaceAccess == "" &&
		len(policy.AllowedTools) == 0 && len(policy.ResultPermissions) == 0 {
		policy = fallback
	}
	result := AgentConfig{
		Policy: PolicyConfig{
			ApprovalMode:      policy.ApprovalMode,
			SandboxMode:       policy.SandboxMode,
			AllowedTools:      append([]domainsecurity.ToolName(nil), policy.AllowedTools...),
			WorkspaceAccess:   policy.WorkspaceAccess,
			ResultPermissions: append([]domainsecurity.ResultPermission(nil), policy.ResultPermissions...),
		},
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
	template, err := domainsecurity.NewDefaultGrantTemplate(
		result.Policy.AllowedTools,
		result.Policy.WorkspaceAccess,
		result.Policy.ResultPermissions,
	)
	if err != nil {
		return AgentConfig{}, fmt.Errorf("invalid policy: %w", err)
	}
	if !result.Policy.ApprovalMode.Valid() || !result.Policy.SandboxMode.Valid() {
		return AgentConfig{}, errors.New("invalid policy approvalMode or sandboxMode")
	}
	result.Policy.AllowedTools = template.AllowedTools
	result.Policy.WorkspaceAccess = template.WorkspaceAccess
	result.Policy.ResultPermissions = template.ResultPermissions
	return result, nil
}

func readFileConfig(path string) (FileConfig, error) {
	encoded, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		config := FileConfig{Agents: defaultAgentConfigs()}
		if err := writeFileConfig(path, config); err != nil {
			return FileConfig{}, err
		}
		return config, nil
	}
	if err != nil {
		return FileConfig{}, fmt.Errorf("read agents config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	var config FileConfig
	if err := decoder.Decode(&config); err != nil {
		return FileConfig{}, fmt.Errorf("agents config: invalid JSON: %w", err)
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return FileConfig{}, fmt.Errorf("agents config: invalid JSON: %w", err)
	}
	return config, nil
}

func writeFileConfig(path string, config FileConfig) error {
	encoded, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		return fmt.Errorf("encode agents config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create agents config directory: %w", err)
	}
	if err := os.WriteFile(path, append(encoded, '\n'), 0o600); err != nil {
		return fmt.Errorf("create agents config: %w", err)
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

func defaultAgentConfigs() map[domainsecurity.AgentProfile]AgentConfig {
	readTools := []domainsecurity.ToolName{
		domainsecurity.ToolReadFile,
		domainsecurity.ToolListDir,
		domainsecurity.ToolSearchText,
	}
	resultTools := []domainsecurity.ToolName{
		domainsecurity.ToolSubmitResult,
		domainsecurity.ToolSubmitBriefing,
	}
	resultPermissions := []domainsecurity.ResultPermission{
		domainsecurity.ResultPermissionAgentResult,
		domainsecurity.ResultPermissionBriefing,
	}
	basePolicy := func(tools []domainsecurity.ToolName, access domainsecurity.WorkspaceAccess, permissions []domainsecurity.ResultPermission) PolicyConfig {
		return PolicyConfig{
			ApprovalMode:      domainsecurity.ApprovalAlwaysAsk,
			SandboxMode:       domainsecurity.SandboxReadOnly,
			AllowedTools:      append([]domainsecurity.ToolName(nil), tools...),
			WorkspaceAccess:   access,
			ResultPermissions: append([]domainsecurity.ResultPermission(nil), permissions...),
		}
	}
	return map[domainsecurity.AgentProfile]AgentConfig{
		domainsecurity.ProfilePrimary: {
			Policy: basePolicy(
				append(append([]domainsecurity.ToolName{}, readTools...), append([]domainsecurity.ToolName{domainsecurity.ToolProposeDelegate}, resultTools...)...),
				domainsecurity.WorkspaceAccessRead,
				resultPermissions,
			),
		},
		domainsecurity.ProfileDelegate: {
			Policy: basePolicy(
				append(append([]domainsecurity.ToolName{}, readTools...), resultTools...),
				domainsecurity.WorkspaceAccessRead,
				resultPermissions,
			),
		},
		domainsecurity.ProfileAdvisor: {
			Policy: basePolicy(
				append(append([]domainsecurity.ToolName{}, readTools...), resultTools...),
				domainsecurity.WorkspaceAccessRead,
				resultPermissions,
			),
		},
		domainsecurity.ProfileCurator: {
			Policy: basePolicy(
				[]domainsecurity.ToolName{domainsecurity.ToolSubmitResult},
				domainsecurity.WorkspaceAccessNone,
				[]domainsecurity.ResultPermission{domainsecurity.ResultPermissionAgentResult},
			),
		},
	}
}
