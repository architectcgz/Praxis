package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/providers/registry"
)

// GetModelConfig returns editable provider metadata and secret presence only.
func (b *ModelBindings) GetModelConfig() (response contracts.ModelConfigDocument, err error) {
	done := b.runtime.begin("GetModelConfig")
	defer func() { done(err) }()
	editor, err := b.configEditor()
	if err != nil {
		return contracts.ModelConfigDocument{}, err
	}
	config := editor.ModelConfig()
	document := contracts.ModelConfigDocument{
		Groups:       make([]contracts.GroupConfigOption, 0, len(config.Groups)),
		Providers:    make([]contracts.ProviderConfigOption, 0, len(config.Providers)),
		Models:       make([]contracts.ModelConfigOption, 0),
		Profiles:     make(map[string]contracts.ModelReference, len(config.Profiles)),
		ProfileNames: registry.ProfileNames(),
	}
	for _, group := range config.Groups {
		document.Groups = append(document.Groups, contracts.GroupConfigOption{
			ID: group.ID, DisplayName: group.DisplayName,
		})
	}
	for _, provider := range config.Providers {
		document.Providers = append(document.Providers, contracts.ProviderConfigOption{
			ID: provider.ID, ProviderName: provider.DisplayName,
			BaseURL: provider.BaseURL, ProxyURL: provider.ProxyURL,
			DefaultAPIFormat: string(provider.DefaultAPIFormat), HasAPIKey: editor.HasProviderKey(provider.ID),
		})
		for _, model := range provider.Models {
			apiFormat := registry.EffectiveAPIFormat(provider, model)
			document.Models = append(document.Models, contracts.ModelConfigOption{
				ProviderID: provider.ID, ModelID: model.ID, Label: model.DisplayName,
				GroupID: model.GroupID, APIFormat: string(apiFormat),
				ContextWindow: model.ContextWindow, MaxOutputTokens: model.MaxOutputTokens,
				Reasoning: contracts.ReasoningOption{
					Supported: model.Reasoning.Supported,
					Levels:    append([]string{}, model.Reasoning.Levels...), Default: model.Reasoning.Default,
				},
			})
		}
	}
	for profile, reference := range config.Profiles {
		document.Profiles[profile] = contracts.ModelReference{ProviderID: reference.ProviderID, ModelID: reference.ModelID}
	}
	return document, nil
}

// SaveModelConfig replaces the whole configuration document. Configuration
// problems the user can fix come back as validationError rather than an opaque
// binding error, because the message is the only way to correct the form.
func (b *ModelBindings) SaveModelConfig(
	request contracts.SaveModelConfigRequest,
) (response contracts.SaveModelConfigResponse, err error) {
	done := b.runtime.begin("SaveModelConfig")
	defer func() { done(err) }()
	editor, err := b.configEditor()
	if err != nil {
		return contracts.SaveModelConfigResponse{}, err
	}
	config := registry.FileConfig{
		Groups:    make([]registry.GroupConfig, 0, len(request.Groups)),
		Providers: make([]registry.ProviderConfig, 0, len(request.Providers)),
		Profiles:  make(map[string]registry.ModelReference, len(request.Profiles)),
	}
	for _, group := range request.Groups {
		config.Groups = append(config.Groups, registry.GroupConfig{
			ID: strings.TrimSpace(group.ID), DisplayName: strings.TrimSpace(group.DisplayName),
		})
	}
	for _, provider := range request.Providers {
		config.Providers = append(config.Providers, registry.ProviderConfig{
			ID: strings.TrimSpace(provider.ID), DisplayName: strings.TrimSpace(provider.ProviderName),
			BaseURL: strings.TrimSpace(provider.BaseURL), ProxyURL: strings.TrimSpace(provider.ProxyURL),
			DefaultAPIFormat: registry.ModelAPIFormat(strings.TrimSpace(provider.DefaultAPIFormat)),
		})
	}
	for _, model := range request.Models {
		providerID := strings.TrimSpace(model.ProviderID)
		apiFormat := registry.ModelAPIFormat(strings.TrimSpace(model.APIFormat))
		modelConfig := registry.ModelConfig{
			ID: strings.TrimSpace(model.ModelID), DisplayName: strings.TrimSpace(model.Label),
			GroupID: strings.TrimSpace(model.GroupID), APIFormatOverride: &apiFormat,
			ContextWindow:   model.ContextWindow,
			MaxOutputTokens: model.MaxOutputTokens,
			Reasoning: registry.ReasoningConfig{
				Supported: model.Reasoning.Supported,
				Levels:    trimmedLevels(model.Reasoning.Levels),
				Default:   strings.TrimSpace(model.Reasoning.Default),
			},
		}
		for index := range config.Providers {
			if config.Providers[index].ID == providerID {
				config.Providers[index].Models = append(config.Providers[index].Models, modelConfig)
				break
			}
		}
	}
	for profile, reference := range request.Profiles {
		reference.ProviderID = strings.TrimSpace(reference.ProviderID)
		reference.ModelID = strings.TrimSpace(reference.ModelID)
		if reference.ProviderID != "" && reference.ModelID != "" {
			config.Profiles[strings.TrimSpace(profile)] = registry.ModelReference{ProviderID: reference.ProviderID, ModelID: reference.ModelID}
		}
	}
	if err := editor.SaveModelConfig(config); err != nil {
		var configuration *registry.ConfigurationError
		if errors.As(err, &configuration) {
			return contracts.SaveModelConfigResponse{}, publicBindingError(err)
		}
		return contracts.SaveModelConfigResponse{ValidationError: err.Error()}, nil
	}
	return contracts.SaveModelConfigResponse{Saved: true}, nil
}

func (b *ModelBindings) SetProviderKey(providerID, value string) (err error) {
	done := b.runtime.begin("SetProviderKey")
	defer func() { done(err) }()
	editor, err := b.configEditor()
	if err != nil {
		return err
	}
	if err := editor.SetProviderKey(strings.TrimSpace(providerID), value); err != nil {
		return publicBindingError(err)
	}
	return nil
}

func (b *ModelBindings) ClearProviderKey(providerID string) error {
	return b.SetProviderKey(providerID, "")
}

// ListProviderModels retrieves the remote model catalog for a saved provider.
// The request runs through the backend using the provider's configured key.
func (b *ModelBindings) ListProviderModels(providerID string) (response []string, err error) {
	done := b.runtime.begin("ListProviderModels")
	defer func() { done(err) }()
	editor, err := b.configEditor()
	if err != nil {
		return nil, err
	}
	ctx, err := b.runtime.context()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	models, err := editor.DiscoverProviderModels(ctx, strings.TrimSpace(providerID))
	if err != nil {
		return nil, err
	}
	return append([]string{}, models...), nil
}

func trimmedLevels(levels []string) []string {
	result := make([]string, 0, len(levels))
	for _, level := range levels {
		if level = strings.TrimSpace(level); level != "" {
			result = append(result, level)
		}
	}
	return result
}
