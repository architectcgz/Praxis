package modelregistry

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	domainmodel "praxis/internal/domain/model"
	"praxis/internal/providers"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidatedConfig 保存已归一化的配置和一次性构造的领域模型。
// 它只能由 Prepare 创建，使 Registry 在执行请求时无需重新构造模型。
type ValidatedConfig struct {
	config   RegistryConfig
	models   map[modelKey]domainmodel.Model
	prepared bool
}

// Config 返回归一化配置的独立副本。
func (c ValidatedConfig) Config() RegistryConfig {
	return cloneRegistryConfig(c.config)
}

// Validate checks a whole model configuration and returns the normalized result
// with defaults applied and reasoning levels deduplicated.
// Loading from disk and saving from the UI share these rules so the two paths
// cannot drift: an error surfaced before saving is the same verdict the next
// startup would reach. The argument is never mutated; the result is a deep copy.
func Validate(config RegistryConfig) (RegistryConfig, error) {
	validated, err := Prepare(config)
	if err != nil {
		return RegistryConfig{}, err
	}
	return validated.Config(), nil
}

// Prepare 归一化配置并一次性构造可直接使用的领域模型。
func Prepare(config RegistryConfig) (ValidatedConfig, error) {
	config = cloneRegistryConfig(config)
	groupsSeen, err := validateGroups(config.Groups)
	if err != nil {
		return ValidatedConfig{}, err
	}
	_, models, err := validateProviders(config.Providers, groupsSeen)
	if err != nil {
		return ValidatedConfig{}, err
	}
	return ValidatedConfig{config: config, models: models, prepared: true}, nil
}

func cloneRegistryConfig(config RegistryConfig) RegistryConfig {
	config.Groups = append([]GroupConfig(nil), config.Groups...)
	config.Providers = append([]ProviderConfig(nil), config.Providers...)
	for index := range config.Providers {
		config.Providers[index].Models = cloneModels(config.Providers[index].Models)
		if credential := config.Providers[index].Credential; credential != nil {
			credentialCopy := *credential
			config.Providers[index].Credential = &credentialCopy
		}
	}
	return config
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

func validateProviders(configs []ProviderConfig, groupsSeen map[string]struct{}) (map[modelKey]struct{}, map[modelKey]domainmodel.Model, error) {
	seen := make(map[string]struct{}, len(configs))
	modelsSeen := make(map[modelKey]struct{})
	models := make(map[modelKey]domainmodel.Model)
	for index := range configs {
		provider := &configs[index]
		provider.ID = strings.TrimSpace(provider.ID)
		provider.DisplayName = strings.TrimSpace(provider.DisplayName)
		provider.BaseURL = strings.TrimSpace(provider.BaseURL)
		provider.ProxyURL = strings.TrimSpace(provider.ProxyURL)
		if !idPattern.MatchString(provider.ID) {
			return nil, nil, fmt.Errorf("models config: invalid provider id %q", provider.ID)
		}
		if _, exists := seen[provider.ID]; exists {
			return nil, nil, fmt.Errorf("models config: duplicate provider id %q", provider.ID)
		}
		seen[provider.ID] = struct{}{}
		if _, err := providers.ValidateBaseURL(provider.BaseURL); err != nil {
			return nil, nil, fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		if _, err := providers.ValidateProxyURL(provider.ProxyURL); err != nil {
			return nil, nil, fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		if provider.DisplayName == "" {
			provider.DisplayName = provider.ID
		}
		if err := validateCredential(provider); err != nil {
			return nil, nil, fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		providerModels, err := validateProviderModels(provider, groupsSeen, modelsSeen)
		if err != nil {
			return nil, nil, err
		}
		for key, model := range providerModels {
			modelsSeen[key] = struct{}{}
			models[key] = model
		}
	}
	return modelsSeen, models, nil
}

func validateCredential(provider *ProviderConfig) error {
	if provider.Credential == nil {
		return nil
	}
	provider.Credential.Type = CredentialType(strings.TrimSpace(string(provider.Credential.Type)))
	provider.Credential.Key = strings.TrimSpace(provider.Credential.Key)
	if provider.Credential.Type != CredentialTypeAPIKey {
		return fmt.Errorf("unsupported credential type %q", provider.Credential.Type)
	}
	if provider.Credential.Key == "" {
		return errors.New("credential key is required")
	}
	return nil
}

func validateProviderModels(provider *ProviderConfig, groupsSeen map[string]struct{}, existing map[modelKey]struct{}) (map[modelKey]domainmodel.Model, error) {
	seen := make(map[modelKey]struct{}, len(provider.Models))
	models := make(map[modelKey]domainmodel.Model, len(provider.Models))
	for index := range provider.Models {
		model := &provider.Models[index]
		model.ID = strings.TrimSpace(model.ID)
		model.DisplayName = strings.TrimSpace(model.DisplayName)
		model.GroupID = strings.TrimSpace(model.GroupID)
		if model.ID == "" {
			return nil, fmt.Errorf("models config: provider %q model id is required", provider.ID)
		}
		if model.DisplayName == "" {
			model.DisplayName = model.ID
		}
		if _, exists := groupsSeen[model.GroupID]; !exists {
			return nil, fmt.Errorf("models config: model %q for provider %q references unknown group %q", model.ID, provider.ID, model.GroupID)
		}
		key := modelKey{provider.ID, model.ID}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("models config: duplicate model %q for provider %q", model.ID, provider.ID)
		}
		if _, exists := existing[key]; exists {
			return nil, fmt.Errorf("models config: duplicate model %q for provider %q", model.ID, provider.ID)
		}
		seen[key] = struct{}{}
		if !validAPIFormat(model.APIFormat) {
			return nil, fmt.Errorf("models config: model %q has unsupported API format %q", model.ID, model.APIFormat)
		}
		domainModel, err := model.DomainModel()
		if err != nil {
			return nil, fmt.Errorf("models config: model %q: %w", model.ID, err)
		}
		model.ID = domainModel.ID
		model.ContextWindow = domainModel.ContextWindow
		model.MaxOutputTokens = domainModel.MaxOutputTokens
		model.ReasoningLevels = domainModel.ReasoningLevels
		model.DefaultReasoningLevel = domainModel.DefaultReasoningLevel
		models[key] = domainModel
	}
	return models, nil
}

func validAPIFormat(format ModelAPIFormat) bool {
	switch format {
	case APIFormatAnthropicMessages, APIFormatOpenAIResponses, APIFormatOpenAIChatCompletions:
		return true
	default:
		return false
	}
}
