package app

import (
	"context"
	"errors"
	"strings"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/providers/registry"
)

// GetModelConfig returns the editable model configuration, including each
// provider's locally configured API key.
func (b *ModelBindings) GetModelConfig() (response contracts.ModelConfigDocument, err error) {
	done := b.runtime.begin("GetModelConfig")
	defer func() { done(err) }()
	editor, err := b.configEditor()
	if err != nil {
		return contracts.ModelConfigDocument{}, err
	}
	config := editor.ModelConfig()
	document := contracts.ModelConfigDocument{
		Providers:    make([]contracts.ProviderConfigOption, 0, len(config.Providers)),
		Models:       make([]contracts.ModelConfigOption, 0, len(config.Models)),
		Profiles:     make(map[string]contracts.ModelReference, len(config.Profiles)),
		ProfileNames: registry.ProfileNames(),
	}
	for _, provider := range config.Providers {
		document.Providers = append(document.Providers, contracts.ProviderConfigOption{
			ID: provider.ID, ProviderName: provider.ProviderName,
			BaseURL: provider.BaseURL, ProxyURL: provider.ProxyURL, APIKey: editor.ProviderKey(provider.ID),
		})
	}
	for _, model := range config.Models {
		document.Models = append(document.Models, contracts.ModelConfigOption{
			ProviderID: model.ProviderID, ModelID: model.ModelID, Label: model.Label,
			APIFormat:     string(model.APIFormat),
			ContextWindow: model.ContextWindow, MaxOutputTokens: model.MaxOutputTokens,
			Reasoning: contracts.ReasoningOption{
				Supported: model.Reasoning.Supported,
				Levels:    append([]string{}, model.Reasoning.Levels...),
				Default:   model.Reasoning.Default,
			},
		})
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
		Providers: make([]registry.ProviderConfig, 0, len(request.Providers)),
		Models:    make([]registry.ModelConfig, 0, len(request.Models)),
		Profiles:  make(map[string]registry.ModelReference, len(request.Profiles)),
	}
	for _, provider := range request.Providers {
		if strings.TrimSpace(provider.APIKey) == "" {
			return contracts.SaveModelConfigResponse{ValidationError: "each provider requires an API key"}, nil
		}
		config.Providers = append(config.Providers, registry.ProviderConfig{
			ID:           strings.TrimSpace(provider.ID),
			ProviderName: strings.TrimSpace(provider.ProviderName),
			BaseURL:      strings.TrimSpace(provider.BaseURL),
			ProxyURL:     strings.TrimSpace(provider.ProxyURL),
		})
	}
	for _, model := range request.Models {
		config.Models = append(config.Models, registry.ModelConfig{
			ProviderID:      strings.TrimSpace(model.ProviderID),
			Label:           strings.TrimSpace(model.Label),
			ModelID:         strings.TrimSpace(model.ModelID),
			APIFormat:       registry.ModelAPIFormat(strings.TrimSpace(model.APIFormat)),
			ContextWindow:   model.ContextWindow,
			MaxOutputTokens: model.MaxOutputTokens,
			Reasoning: registry.ReasoningConfig{
				Supported: model.Reasoning.Supported,
				Levels:    trimmedLevels(model.Reasoning.Levels),
				Default:   strings.TrimSpace(model.Reasoning.Default),
			},
		})
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
	for _, provider := range request.Providers {
		if err := editor.SetProviderKey(strings.TrimSpace(provider.ID), provider.APIKey); err != nil {
			var configuration *registry.ConfigurationError
			if errors.As(err, &configuration) {
				return contracts.SaveModelConfigResponse{}, publicBindingError(err)
			}
			return contracts.SaveModelConfigResponse{ValidationError: err.Error()}, nil
		}
	}
	return contracts.SaveModelConfigResponse{Saved: true}, nil
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
