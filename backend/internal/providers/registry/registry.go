// Package registry loads the user-owned model and secret configuration and
// materializes provider-neutral model ports.
package registry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"

	"praxis/internal/core/domain"
	coreruntime "praxis/internal/core/runtime"
	"praxis/internal/providers"
	"praxis/internal/providers/anthropic"
	"praxis/internal/providers/openaicompat"
)

const currentVersion = 1

type ProviderKind string

const (
	KindAnthropic        ProviderKind = "anthropic"
	KindOpenAICompatible ProviderKind = "openai_compatible"
)

type ProviderConfig struct {
	ID        string       `json:"id"`
	Label     string       `json:"label"`
	Kind      ProviderKind `json:"kind"`
	BaseURL   string       `json:"baseURL"`
	APIKeyEnv string       `json:"apiKeyEnv"`
	Protocol  string       `json:"protocol,omitempty"`
}

type ModelConfig struct {
	ID              string          `json:"id"`
	ProviderID      string          `json:"providerId"`
	Label           string          `json:"label"`
	Model           string          `json:"model"`
	ContextWindow   int             `json:"contextWindow"`
	MaxOutputTokens int             `json:"maxOutputTokens"`
	Reasoning       ReasoningConfig `json:"reasoning,omitempty"`
	LegacyEffort    string          `json:"effort,omitempty"`
}

// ReasoningConfig declares the levels a confirmed model accepts. The provider
// adapter maps an approved level to its protocol-specific request field.
type ReasoningConfig struct {
	Supported bool     `json:"supported"`
	Levels    []string `json:"levels,omitempty"`
	Default   string   `json:"default,omitempty"`
}

// ModelOption is the safe model catalog exposed to desktop clients. It omits
// provider endpoints, API key locations, and the provider-side model ID.
type ModelOption struct {
	ID              string
	Label           string
	ProviderLabel   string
	Reasoning       ReasoningConfig
	DefaultProfiles []string
}

type FileConfig struct {
	Version   int               `json:"version"`
	Providers []ProviderConfig  `json:"providers"`
	Models    []ModelConfig     `json:"models"`
	Profiles  map[string]string `json:"profiles"`
}

type secretsFile struct {
	Version int               `json:"version"`
	Keys    map[string]string `json:"keys"`
}

type Registry struct {
	config     FileConfig
	secrets    map[string]string
	client     *http.Client
	byModel    map[string]ModelConfig
	byProvider map[string]ProviderConfig
}

// ConfigurationError identifies the configuration file that prevented the
// registry from loading. Its cause never contains a resolved secret value.
type ConfigurationError struct {
	Path string
	Err  error
}

func (e *ConfigurationError) Error() string {
	if e == nil {
		return "configuration error"
	}
	return fmt.Sprintf("configuration %q: %v", e.Path, e.Err)
}

func (e *ConfigurationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

var envPattern = regexp.MustCompile(`^[A-Z][A-Z0-9_]*$`)
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func Load(modelsPath, secretsPath string, client *http.Client) (*Registry, error) {
	config, err := loadModels(modelsPath)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	secrets, err := loadSecrets(secretsPath)
	if err != nil {
		return nil, &ConfigurationError{Path: secretsPath, Err: err}
	}
	registry := &Registry{
		config: config, secrets: secrets, client: client,
		byModel: make(map[string]ModelConfig), byProvider: make(map[string]ProviderConfig),
	}
	for _, provider := range config.Providers {
		registry.byProvider[provider.ID] = provider
	}
	for _, model := range config.Models {
		registry.byModel[model.ID] = model
	}
	return registry, nil
}

func (r *Registry) Config() FileConfig {
	copy := r.config
	copy.Providers = append([]ProviderConfig(nil), r.config.Providers...)
	copy.Models = cloneModels(r.config.Models)
	copy.Profiles = make(map[string]string, len(r.config.Profiles))
	for key, value := range r.config.Profiles {
		copy.Profiles[key] = value
	}
	return copy
}

func cloneModels(models []ModelConfig) []ModelConfig {
	copy := append([]ModelConfig(nil), models...)
	for index := range copy {
		copy[index].Reasoning.Levels = append([]string(nil), models[index].Reasoning.Levels...)
	}
	return copy
}

func (r *Registry) Model(id string) (ModelConfig, error) {
	model, ok := r.byModel[strings.TrimSpace(id)]
	if !ok {
		return ModelConfig{}, fmt.Errorf("model %q is not configured", id)
	}
	return model, nil
}

// ListModels returns confirmed models that may be selected for the next
// execution. The profile list identifies configured defaults only.
func (r *Registry) ListModels() []ModelOption {
	options := make([]ModelOption, 0, len(r.config.Models))
	for _, model := range r.config.Models {
		provider := r.byProvider[model.ProviderID]
		profiles := make([]string, 0, len(r.config.Profiles))
		for profile, modelID := range r.config.Profiles {
			if modelID == model.ID {
				profiles = append(profiles, profile)
			}
		}
		sort.Strings(profiles)
		label := strings.TrimSpace(model.Label)
		if label == "" {
			label = model.Model
		}
		providerLabel := strings.TrimSpace(provider.Label)
		if providerLabel == "" {
			providerLabel = provider.ID
		}
		options = append(options, ModelOption{
			ID: model.ID, Label: label, ProviderLabel: providerLabel,
			Reasoning: cloneReasoning(model.Reasoning), DefaultProfiles: profiles,
		})
	}
	sort.Slice(options, func(i, j int) bool {
		if options[i].Label == options[j].Label {
			return options[i].ID < options[j].ID
		}
		return options[i].Label < options[j].Label
	})
	return options
}

// ResolveModel returns the configured model ID for an agent profile. Profiles
// are resolved when a Grant is created so durable executions retain the exact
// model selection that was approved for them.
func (r *Registry) ResolveModel(profile domain.AgentProfile) (domain.ModelRef, error) {
	if !profile.Valid() {
		return domain.ModelRef{}, fmt.Errorf("unknown agent profile %q", profile)
	}
	modelID, ok := r.config.Profiles[string(profile)]
	if !ok || strings.TrimSpace(modelID) == "" {
		return domain.ModelRef{}, fmt.Errorf("model profile %q is not configured", profile)
	}
	model, err := r.Model(modelID)
	if err != nil {
		return domain.ModelRef{}, fmt.Errorf("model profile %q: %w", profile, err)
	}
	return r.modelRef(model, "")
}

// ResolveModelSelection validates a model and optional thinking level before
// the orchestration layer freezes it into an execution-specific Grant.
func (r *Registry) ResolveModelSelection(modelID, reasoning string) (domain.ModelRef, error) {
	model, err := r.Model(modelID)
	if err != nil {
		return domain.ModelRef{}, err
	}
	return r.modelRef(model, reasoning)
}

func (r *Registry) StreamPort(modelID string) (coreruntime.ModelStreamPort, error) {
	return r.StreamPortFor(domain.ModelRef{ID: modelID})
}

// StreamPortFor builds the port for the already-frozen model selection.
func (r *Registry) StreamPortFor(ref domain.ModelRef) (coreruntime.ModelStreamPort, error) {
	if strings.TrimSpace(ref.ID) == "" {
		return nil, errors.New("model reference is required")
	}
	model, err := r.Model(ref.ID)
	if err != nil {
		return nil, err
	}
	selection, err := r.modelRef(model, ref.Reasoning)
	if err != nil {
		return nil, err
	}
	provider, ok := r.byProvider[model.ProviderID]
	if !ok {
		return nil, fmt.Errorf("provider %q is not configured", model.ProviderID)
	}
	resolve := r.resolveKey
	switch provider.Kind {
	case KindAnthropic:
		return anthropic.New(anthropic.Config{
			BaseURL: provider.BaseURL, APIKeyEnv: provider.APIKeyEnv,
			ResolveKey: resolve, HTTPClient: r.client,
			Model: model.Model, MaxOutputTokens: model.MaxOutputTokens,
			Reasoning: selection.Reasoning,
		})
	case KindOpenAICompatible:
		return openaicompat.New(openaicompat.Config{
			BaseURL: provider.BaseURL, APIKeyEnv: provider.APIKeyEnv,
			ResolveKey: resolve, HTTPClient: r.client,
			Protocol: openaicompat.Protocol(provider.Protocol),
			Model:    model.Model, MaxOutputTokens: model.MaxOutputTokens,
			Reasoning: selection.Reasoning,
		})
	default:
		return nil, fmt.Errorf("provider %q has unsupported kind %q", provider.ID, provider.Kind)
	}
}

func (r *Registry) modelRef(model ModelConfig, reasoning string) (domain.ModelRef, error) {
	reasoning = strings.TrimSpace(reasoning)
	config := model.Reasoning
	if !config.Supported {
		if reasoning != "" {
			return domain.ModelRef{}, fmt.Errorf("model %q does not support reasoning", model.ID)
		}
		return domain.ModelRef{ID: model.ID}, nil
	}
	if reasoning == "" {
		reasoning = config.Default
	}
	if !containsReasoningLevel(config.Levels, reasoning) {
		return domain.ModelRef{}, fmt.Errorf("model %q does not support reasoning %q", model.ID, reasoning)
	}
	return domain.ModelRef{ID: model.ID, Reasoning: reasoning}, nil
}

func cloneReasoning(config ReasoningConfig) ReasoningConfig {
	return ReasoningConfig{
		Supported: config.Supported,
		Levels:    append([]string(nil), config.Levels...),
		Default:   config.Default,
	}
}

func containsReasoningLevel(levels []string, target string) bool {
	for _, level := range levels {
		if level == target {
			return true
		}
	}
	return false
}

func normalizeReasoning(config ReasoningConfig) (ReasoningConfig, error) {
	config.Default = strings.TrimSpace(config.Default)
	if !config.Supported {
		if len(config.Levels) > 0 || config.Default != "" {
			return ReasoningConfig{}, errors.New("unsupported reasoning cannot declare levels or a default")
		}
		return ReasoningConfig{}, nil
	}
	if len(config.Levels) == 0 {
		return ReasoningConfig{}, errors.New("supported reasoning requires levels")
	}
	levels := make([]string, 0, len(config.Levels))
	seen := make(map[string]struct{}, len(config.Levels))
	for _, level := range config.Levels {
		level = strings.TrimSpace(level)
		if level == "" {
			return ReasoningConfig{}, errors.New("reasoning levels cannot be empty")
		}
		if _, exists := seen[level]; exists {
			return ReasoningConfig{}, fmt.Errorf("duplicate reasoning level %q", level)
		}
		seen[level] = struct{}{}
		levels = append(levels, level)
	}
	if config.Default == "" {
		config.Default = "medium"
	}
	if !containsReasoningLevel(levels, config.Default) {
		return ReasoningConfig{}, fmt.Errorf("reasoning default %q is not supported", config.Default)
	}
	config.Levels = levels
	return config, nil
}

func hasNoReasoningConfig(config ReasoningConfig) bool {
	return !config.Supported && len(config.Levels) == 0 && config.Default == ""
}

func (r *Registry) resolveKey(ctx context.Context, name string) (string, error) {
	if ctx == nil {
		return "", errors.New("key resolver context is required")
	}
	if !envPattern.MatchString(name) {
		return "", fmt.Errorf("invalid API key environment name %q", name)
	}
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value, nil
	}
	if value := strings.TrimSpace(r.secrets[name]); value != "" {
		return value, nil
	}
	return "", fmt.Errorf("API key is not configured for %s", name)
}

func loadModels(path string) (FileConfig, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		config := FileConfig{
			Version: currentVersion, Providers: []ProviderConfig{}, Models: []ModelConfig{},
			Profiles: map[string]string{},
		}
		payload, marshalErr := json.MarshalIndent(config, "", "  ")
		if marshalErr != nil {
			return FileConfig{}, fmt.Errorf("encode models config template: %w", marshalErr)
		}
		if writeErr := os.WriteFile(path, append(payload, '\n'), 0o600); writeErr != nil {
			return FileConfig{}, fmt.Errorf("create models config: %w", writeErr)
		}
		file, err = os.Open(path)
	}
	if err != nil {
		return FileConfig{}, fmt.Errorf("open models config: %w", err)
	}
	defer file.Close()
	var config FileConfig
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return FileConfig{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return FileConfig{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if config.Version != currentVersion {
		return FileConfig{}, fmt.Errorf("models config: unsupported version %d", config.Version)
	}
	if config.Profiles == nil {
		config.Profiles = map[string]string{}
	}
	providersSeen := map[string]struct{}{}
	for _, provider := range config.Providers {
		if !idPattern.MatchString(provider.ID) {
			return FileConfig{}, fmt.Errorf("models config: invalid provider id %q", provider.ID)
		}
		if _, exists := providersSeen[provider.ID]; exists {
			return FileConfig{}, fmt.Errorf("models config: duplicate provider id %q", provider.ID)
		}
		providersSeen[provider.ID] = struct{}{}
		if provider.Kind != KindAnthropic && provider.Kind != KindOpenAICompatible {
			return FileConfig{}, fmt.Errorf(
				"models config: provider %q has unsupported kind %q", provider.ID, provider.Kind,
			)
		}
		if provider.Protocol != "" && provider.Protocol != string(openaicompat.ProtocolChatCompletions) &&
			provider.Protocol != string(openaicompat.ProtocolResponses) {
			return FileConfig{}, fmt.Errorf(
				"models config: provider %q has unsupported protocol %q",
				provider.ID, provider.Protocol,
			)
		}
		if provider.Kind != KindOpenAICompatible && provider.Protocol != "" {
			return FileConfig{}, fmt.Errorf(
				"models config: provider %q protocol is only valid for openai_compatible",
				provider.ID,
			)
		}
		if provider.APIKeyEnv == "" || !envPattern.MatchString(provider.APIKeyEnv) {
			return FileConfig{}, fmt.Errorf("models config: provider %q has invalid apiKeyEnv", provider.ID)
		}
		if _, err := providers.ValidateBaseURL(provider.BaseURL); err != nil {
			return FileConfig{}, fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
	}
	modelsSeen := map[string]struct{}{}
	for index := range config.Models {
		model := &config.Models[index]
		if !idPattern.MatchString(model.ID) {
			return FileConfig{}, fmt.Errorf("models config: invalid model id %q", model.ID)
		}
		if _, exists := modelsSeen[model.ID]; exists {
			return FileConfig{}, fmt.Errorf("models config: duplicate model id %q", model.ID)
		}
		modelsSeen[model.ID] = struct{}{}
		if _, exists := providersSeen[model.ProviderID]; !exists {
			return FileConfig{}, fmt.Errorf(
				"models config: model %q references unknown provider %q", model.ID, model.ProviderID,
			)
		}
		if strings.TrimSpace(model.Model) == "" || model.ContextWindow <= 0 ||
			model.MaxOutputTokens <= 0 || model.MaxOutputTokens >= model.ContextWindow {
			return FileConfig{}, fmt.Errorf("models config: model %q has invalid capability limits", model.ID)
		}
		if hasNoReasoningConfig(model.Reasoning) && strings.TrimSpace(model.LegacyEffort) != "" {
			legacyDefault := strings.TrimSpace(model.LegacyEffort)
			model.Reasoning = ReasoningConfig{
				Supported: true,
				Levels:    []string{legacyDefault},
				Default:   legacyDefault,
			}
		}
		normalizedReasoning, reasoningErr := normalizeReasoning(model.Reasoning)
		if reasoningErr != nil {
			return FileConfig{}, fmt.Errorf("models config: model %q: %w", model.ID, reasoningErr)
		}
		model.Reasoning = normalizedReasoning
	}
	for profile, modelID := range config.Profiles {
		if profile != "primary" && profile != "delegate" && profile != "consult" && profile != "note" {
			return FileConfig{}, fmt.Errorf("models config: unknown profile %q", profile)
		}
		if _, exists := modelsSeen[modelID]; !exists {
			return FileConfig{}, fmt.Errorf("models config: profiles.%s references unknown model %q", profile, modelID)
		}
	}
	return config, nil
}

func loadSecrets(path string) (map[string]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("open secrets config: %w", err)
	}
	defer file.Close()
	var config secretsFile
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("secrets config: invalid JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, fmt.Errorf("secrets config: invalid JSON: %w", err)
	}
	if config.Version != currentVersion {
		return nil, fmt.Errorf("secrets config: unsupported version %d", config.Version)
	}
	for name := range config.Keys {
		if !envPattern.MatchString(name) {
			return nil, fmt.Errorf("secrets config: invalid key name %q", name)
		}
	}
	return config.Keys, nil
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}
