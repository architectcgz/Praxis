package bindings

import (
	"context"
	"errors"
	"time"

	appmodelconfig "praxis/internal/modelconfig"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// GetModelConfig returns editable provider metadata and secret presence only.
func (b *ModelBindings) GetModelConfig() (dto.ModelConfigDocument, error) {
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.ModelConfigDocument{}, err
	}
	if err := ctx.Err(); err != nil {
		return dto.ModelConfigDocument{}, publicError(b.runtime, "ModelBindings.GetModelConfig.context", err)
	}
	editor := service.ModelConfig
	config := editor.ModelConfig()
	document := dto.ModelConfigDocument{
		Groups:            make([]dto.GroupConfigOption, 0, len(config.Groups)),
		DefaultProviderID: config.DefaultProviderID,
		Providers:         make([]dto.ProviderConfigOption, 0, len(config.Providers)),
		Models:            make([]dto.ModelConfigOption, 0),
	}
	for _, group := range config.Groups {
		document.Groups = append(document.Groups, dto.GroupConfigOption{
			ID: group.ID, DisplayName: group.DisplayName,
		})
	}
	for _, provider := range config.Providers {
		document.Providers = append(document.Providers, dto.ProviderConfigOption{
			ID: provider.ID, ProviderName: provider.DisplayName,
			BaseURL: provider.BaseURL, ProxyURL: provider.ProxyURL,
			DefaultModelID: provider.DefaultModelID,
			HasAPIKey:      editor.HasProviderKey(provider.ID),
		})
		for _, configured := range provider.Models {
			document.Models = append(document.Models, dto.ModelConfigOption{
				ProviderID: provider.ID, ModelID: configured.ID, Label: configured.DisplayName,
				GroupID: configured.GroupID, APIFormat: string(configured.APIFormat),
				ContextWindow: configured.ContextWindow, MaxOutputTokens: configured.MaxOutputTokens,
				ReasoningLevels:       append([]string{}, configured.ReasoningLevels...),
				DefaultReasoningLevel: configured.DefaultReasoningLevel,
			})
		}
	}
	return document, nil
}

// SaveModelConfig replaces the whole configuration document.
func (b *ModelBindings) SaveModelConfig(request dto.SaveModelConfigRequest) (dto.SaveModelConfigResponse, error) {
	if err := validation.ValidateSaveModelConfig(request); err != nil {
		return dto.SaveModelConfigResponse{}, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SaveModelConfigResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return dto.SaveModelConfigResponse{}, publicError(b.runtime, "ModelBindings.SaveModelConfig.context", err)
	}
	config := appmodelconfig.Config{
		Groups:            make([]appmodelconfig.Group, 0, len(request.Groups)),
		DefaultProviderID: request.DefaultProviderID,
		Providers:         make([]appmodelconfig.Provider, 0, len(request.Providers)),
	}
	for _, group := range request.Groups {
		config.Groups = append(config.Groups, appmodelconfig.Group{
			ID: group.ID, DisplayName: group.DisplayName,
		})
	}
	for _, provider := range request.Providers {
		config.Providers = append(config.Providers, appmodelconfig.Provider{
			ID: provider.ID, DisplayName: provider.ProviderName,
			BaseURL: provider.BaseURL, ProxyURL: provider.ProxyURL,
			DefaultModelID: provider.DefaultModelID,
		})
	}
	for _, option := range request.Models {
		providerID := option.ProviderID
		configured := appmodelconfig.Model{
			ID: option.ModelID, DisplayName: option.Label,
			GroupID:       option.GroupID,
			APIFormat:     appmodelconfig.APIFormat(option.APIFormat),
			ContextWindow: option.ContextWindow, MaxOutputTokens: option.MaxOutputTokens,
			ReasoningLevels:       append([]string(nil), option.ReasoningLevels...),
			DefaultReasoningLevel: option.DefaultReasoningLevel,
		}
		for index := range config.Providers {
			if config.Providers[index].ID == providerID {
				config.Providers[index].Models = append(config.Providers[index].Models, configured)
				break
			}
		}
	}
	if err := service.ModelConfig.SaveModelConfig(config); err != nil {
		var validationError *appmodelconfig.ValidationError
		if errors.As(err, &validationError) {
			return dto.SaveModelConfigResponse{ValidationError: err.Error()}, nil
		}
		return dto.SaveModelConfigResponse{}, publicError(b.runtime, "ModelBindings.SaveModelConfig", err)
	}
	return dto.SaveModelConfigResponse{Saved: true}, nil
}

// SetProviderKey stores or clears the API key for one provider.
func (b *ModelBindings) SetProviderKey(providerID, value string) error {
	if err := validation.ValidateProviderID(providerID); err != nil {
		return err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return publicError(b.runtime, "ModelBindings.SetProviderKey.context", err)
	}
	if err := service.ModelConfig.SetProviderKey(providerID, value); err != nil {
		return publicError(b.runtime, "ModelBindings.SetProviderKey", err)
	}
	return nil
}

// ClearProviderKey clears the API key for one provider.
func (b *ModelBindings) ClearProviderKey(providerID string) error {
	return b.SetProviderKey(providerID, "")
}

// ListProviderModels retrieves the remote model catalog for a saved provider.
func (b *ModelBindings) ListProviderModels(providerID string) ([]string, error) {
	if err := validation.ValidateProviderID(providerID); err != nil {
		return nil, err
	}
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	models, err := service.ModelConfig.DiscoverProviderModels(ctx, providerID)
	if err != nil {
		return nil, publicError(b.runtime, "ModelBindings.ListProviderModels", err)
	}
	return append([]string{}, models...), nil
}

// ReloadConfig 从磁盘重新读取 Praxis 配置；失败时保留原运行时配置。
func (b *ModelBindings) ReloadConfig() error {
	ctx, service, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	if err := service.ModelConfig.ReloadConfig(ctx); err != nil {
		return publicError(b.runtime, "ModelBindings.ReloadConfig", err)
	}
	return nil
}
