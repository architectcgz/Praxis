package modelregistry

import (
	"fmt"
	"praxis/internal/infra/providers"
	"regexp"
	"strings"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidatedConfig is a normalized configuration that passed Prepare. Only
// Prepare can create it, which is the guard ApplyValidatedConfig relies on
// before replacing the in-memory indexes.
type ValidatedConfig struct {
	config   RegistryConfig
	prepared bool
}

// Config 返回归一化配置的独立副本。
func (c ValidatedConfig) Config() RegistryConfig {
	return cloneRegistryConfig(c.config)
}

// Prepare validates and normalizes a configuration before the Registry may
// apply it. The returned value carries the prepared marker that
// ApplyValidatedConfig requires.
func Prepare(config RegistryConfig) (ValidatedConfig, error) {
	normalized := cloneRegistryConfig(config)
	if err := validateRegistryConfig(&normalized); err != nil {
		return ValidatedConfig{}, err
	}
	return ValidatedConfig{config: normalized, prepared: true}, nil
}

func cloneRegistryConfig(config RegistryConfig) RegistryConfig {
	config.Groups = append([]GroupConfig(nil), config.Groups...)
	config.Providers = append([]ProviderConfig(nil), config.Providers...)
	for index := range config.Providers {
		config.Providers[index].Models = cloneModels(config.Providers[index].Models)
	}
	return config
}

// validateRegistryConfig normalizes a cloned configuration in place and
// checks every structural and domain rule.
func validateRegistryConfig(config *RegistryConfig) error {
	config.DefaultProviderID = strings.TrimSpace(config.DefaultProviderID)
	groupsSeen, err := validateGroups(config.Groups)
	if err != nil {
		return err
	}
	if err := validateProviders(config.Providers, groupsSeen); err != nil {
		return err
	}
	if config.DefaultProviderID == "" {
		if len(config.Providers) > 0 {
			return fmt.Errorf("models config: default provider is required")
		}
		return nil
	}
	if !containsProvider(config.Providers, config.DefaultProviderID) {
		return fmt.Errorf("models config: default provider %q is not configured", config.DefaultProviderID)
	}
	return nil
}

func validateGroups(configs []GroupConfig) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(configs))
	for index := range configs {
		group := &configs[index]
		group.ID = strings.TrimSpace(group.ID)
		group.DisplayName = strings.TrimSpace(group.DisplayName)
		if !idPattern.MatchString(group.ID) {
			return nil, fmt.Errorf("models config: invalid group id %q", group.ID)
		}
		if group.DisplayName == "" {
			return nil, fmt.Errorf("models config: group %q display name is required", group.ID)
		}
		if _, exists := seen[group.ID]; exists {
			return nil, fmt.Errorf("models config: duplicate group id %q", group.ID)
		}
		seen[group.ID] = struct{}{}
	}
	return seen, nil
}

func validateProviders(configs []ProviderConfig, groupsSeen map[string]struct{}) error {
	seen := make(map[string]struct{}, len(configs))
	modelsSeen := make(map[modelKey]struct{})
	for index := range configs {
		provider := &configs[index]
		provider.ID = strings.TrimSpace(provider.ID)
		provider.DisplayName = strings.TrimSpace(provider.DisplayName)
		provider.DefaultModelID = strings.TrimSpace(provider.DefaultModelID)
		if !idPattern.MatchString(provider.ID) {
			return fmt.Errorf("models config: invalid provider id %q", provider.ID)
		}
		if _, exists := seen[provider.ID]; exists {
			return fmt.Errorf("models config: duplicate provider id %q", provider.ID)
		}
		seen[provider.ID] = struct{}{}
		baseURL, err := providers.ValidateBaseURL(provider.BaseURL)
		if err != nil {
			return fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		provider.BaseURL = baseURL
		proxyURL, err := providers.ValidateProxyURL(provider.ProxyURL)
		if err != nil {
			return fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		provider.ProxyURL = proxyURL
		if provider.DisplayName == "" {
			provider.DisplayName = provider.ID
		}
		if err := validateProviderModels(provider, groupsSeen, modelsSeen); err != nil {
			return err
		}
		if provider.DefaultModelID != "" && !containsModel(provider.Models, provider.DefaultModelID) {
			return fmt.Errorf("models config: provider %q default model %q is not configured", provider.ID, provider.DefaultModelID)
		}
	}
	return nil
}

func containsModel(models []ModelConfig, modelID string) bool {
	for _, model := range models {
		if model.ID == modelID {
			return true
		}
	}
	return false
}

func validateProviderModels(provider *ProviderConfig, groupsSeen map[string]struct{}, existing map[modelKey]struct{}) error {
	seen := make(map[modelKey]struct{}, len(provider.Models))
	for index := range provider.Models {
		model := &provider.Models[index]
		model.ID = strings.TrimSpace(model.ID)
		model.DisplayName = strings.TrimSpace(model.DisplayName)
		model.GroupID = strings.TrimSpace(model.GroupID)
		model.APIFormat = ModelAPIFormat(strings.TrimSpace(string(model.APIFormat)))
		if model.ID == "" {
			return fmt.Errorf("models config: provider %q model id is required", provider.ID)
		}
		if model.DisplayName == "" {
			model.DisplayName = model.ID
		}
		if _, exists := groupsSeen[model.GroupID]; !exists {
			return fmt.Errorf("models config: model %q for provider %q references unknown group %q", model.ID, provider.ID, model.GroupID)
		}
		key := modelKey{provider.ID, model.ID}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("models config: duplicate model %q for provider %q", model.ID, provider.ID)
		}
		if _, exists := existing[key]; exists {
			return fmt.Errorf("models config: duplicate model %q for provider %q", model.ID, provider.ID)
		}
		seen[key] = struct{}{}
		existing[key] = struct{}{}
		if !validAPIFormat(model.APIFormat) {
			return fmt.Errorf("models config: model %q has unsupported API format %q", model.ID, model.APIFormat)
		}
		if err := model.normalizeCapabilities(); err != nil {
			return fmt.Errorf("models config: model %q: %w", model.ID, err)
		}
	}
	return nil
}

func validAPIFormat(format ModelAPIFormat) bool {
	switch format {
	case APIFormatAnthropicMessages, APIFormatOpenAIResponses, APIFormatOpenAIChatCompletions:
		return true
	default:
		return false
	}
}

func containsProvider(providers []ProviderConfig, providerID string) bool {
	for _, provider := range providers {
		if provider.ID == providerID {
			return true
		}
	}
	return false
}
