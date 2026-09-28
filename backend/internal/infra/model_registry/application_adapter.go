package modelregistry

import (
	context "context"

	appconfig "praxis/internal/modelconfig"
)

// ApplicationAdapter 将模型注册表适配为 application 设置用例端口。
type ApplicationAdapter struct {
	registry *Registry
}

func NewApplicationAdapter(registry *Registry) *ApplicationAdapter {
	return &ApplicationAdapter{registry: registry}
}

func (a *ApplicationAdapter) ListModels() []appconfig.Option {
	if a == nil || a.registry == nil {
		return []appconfig.Option{}
	}
	options := a.registry.ListModels()
	result := make([]appconfig.Option, len(options))
	for index, option := range options {
		result[index] = appconfig.Option{
			ProviderID:               option.ProviderID,
			ModelID:                  option.ModelID,
			DefaultProviderID:        option.DefaultProviderID,
			DefaultModelID:           option.DefaultModelID,
			Label:                    option.Label,
			ProviderName:             option.ProviderName,
			ReasoningLevels:          append([]string(nil), option.ReasoningLevels...),
			DefaultReasoningLevel:    option.DefaultReasoningLevel,
			AssignedAgentDefinitions: append([]string(nil), option.AssignedAgentDefinitions...),
		}
	}
	return result
}

func (a *ApplicationAdapter) Config() appconfig.Config {
	if a == nil || a.registry == nil {
		return appconfig.Config{}
	}
	return applicationConfig(a.registry.Config())
}

func (a *ApplicationAdapter) Save(config appconfig.Config) error {
	if a == nil || a.registry == nil {
		return &appconfig.ValidationError{}
	}
	prepared, err := Prepare(registryConfig(config))
	if err != nil {
		return &appconfig.ValidationError{Err: err}
	}
	return a.registry.ApplyValidatedConfig(prepared)
}

func (a *ApplicationAdapter) SetProviderKey(providerID, value string) error {
	if a == nil || a.registry == nil {
		return &appconfig.ValidationError{}
	}
	return a.registry.SetProviderKey(providerID, value)
}

func (a *ApplicationAdapter) HasProviderKey(providerID string) bool {
	return a != nil && a.registry != nil && a.registry.HasProviderKey(providerID)
}

func (a *ApplicationAdapter) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
	if a == nil || a.registry == nil {
		return nil, &appconfig.ValidationError{}
	}
	return a.registry.DiscoverProviderModels(ctx, providerID)
}

func applicationConfig(config RegistryConfig) appconfig.Config {
	result := appconfig.Config{
		Groups:            make([]appconfig.Group, len(config.Groups)),
		DefaultProviderID: config.DefaultProviderID,
		Providers:         make([]appconfig.Provider, len(config.Providers)),
	}
	for index, group := range config.Groups {
		result.Groups[index] = appconfig.Group{ID: group.ID, DisplayName: group.DisplayName}
	}
	for providerIndex, provider := range config.Providers {
		models := make([]appconfig.Model, len(provider.Models))
		for modelIndex, configured := range provider.Models {
			models[modelIndex] = appconfig.Model{
				ID:                    configured.ID,
				DisplayName:           configured.DisplayName,
				GroupID:               configured.GroupID,
				APIFormat:             appconfig.APIFormat(configured.APIFormat),
				ContextWindow:         configured.ContextWindow,
				MaxOutputTokens:       configured.MaxOutputTokens,
				ReasoningLevels:       append([]string(nil), configured.ReasoningLevels...),
				DefaultReasoningLevel: configured.DefaultReasoningLevel,
			}
		}
		result.Providers[providerIndex] = appconfig.Provider{
			ID:             provider.ID,
			DisplayName:    provider.DisplayName,
			BaseURL:        provider.BaseURL,
			ProxyURL:       provider.ProxyURL,
			DefaultModelID: provider.DefaultModelID,
			Models:         models,
		}
	}
	return result
}

func registryConfig(config appconfig.Config) RegistryConfig {
	result := RegistryConfig{
		Groups:            make([]GroupConfig, len(config.Groups)),
		DefaultProviderID: config.DefaultProviderID,
		Providers:         make([]ProviderConfig, len(config.Providers)),
	}
	for index, group := range config.Groups {
		result.Groups[index] = GroupConfig{ID: group.ID, DisplayName: group.DisplayName}
	}
	for providerIndex, provider := range config.Providers {
		models := make([]ModelConfig, len(provider.Models))
		for modelIndex, configured := range provider.Models {
			models[modelIndex] = ModelConfig{
				ID:                    configured.ID,
				DisplayName:           configured.DisplayName,
				GroupID:               configured.GroupID,
				APIFormat:             ModelAPIFormat(configured.APIFormat),
				ContextWindow:         configured.ContextWindow,
				MaxOutputTokens:       configured.MaxOutputTokens,
				ReasoningLevels:       append([]string(nil), configured.ReasoningLevels...),
				DefaultReasoningLevel: configured.DefaultReasoningLevel,
			}
		}
		result.Providers[providerIndex] = ProviderConfig{
			ID:             provider.ID,
			DisplayName:    provider.DisplayName,
			BaseURL:        provider.BaseURL,
			ProxyURL:       provider.ProxyURL,
			DefaultModelID: provider.DefaultModelID,
			Models:         models,
		}
	}
	return result
}

var _ appconfig.Catalog = (*ApplicationAdapter)(nil)
var _ appconfig.Editor = (*ApplicationAdapter)(nil)
