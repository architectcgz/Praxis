package bindings

import (
	"context"
	"errors"
	"time"

	"praxis/internal/contracts"
	"praxis/internal/request"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// GetModelConfig 返回可编辑模型元数据和密钥存在状态，不返回密钥内容。
func (b *ModelBindings) GetModelConfig() (dto.ModelConfigDocument, error) {
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.ModelConfigDocument{}, err
	}
	if err := ctx.Err(); err != nil {
		return dto.ModelConfigDocument{}, publicError(b.runtime, "ModelBindings.GetModelConfig.context", err)
	}
	config := services.ModelConfig.ModelConfig()
	document := dto.ModelConfigDocument{
		Groups:            make([]dto.GroupConfigOption, 0, len(config.Groups)),
		DefaultProviderID: config.DefaultProviderID,
		Providers:         make([]dto.ProviderConfigOption, 0, len(config.Providers)),
		Models:            make([]dto.ModelConfigOption, 0, len(config.Models)),
	}
	for _, group := range config.Groups {
		document.Groups = append(document.Groups, dto.GroupConfigOption{
			ID:          group.ID,
			DisplayName: group.DisplayName,
		})
	}
	for _, provider := range config.Providers {
		document.Providers = append(document.Providers, dto.ProviderConfigOption{
			ID:             provider.ID,
			ProviderName:   provider.DisplayName,
			BaseURL:        provider.BaseURL,
			ProxyURL:       provider.ProxyURL,
			DefaultModelID: provider.DefaultModelID,
			HasAPIKey:      provider.HasAPIKey,
		})
	}
	for _, model := range config.Models {
		document.Models = append(document.Models, dto.ModelConfigOption{
			ProviderID:            model.ProviderID,
			ModelID:               model.ModelID,
			Label:                 model.DisplayName,
			GroupID:               model.GroupID,
			APIFormat:             model.APIFormat,
			ContextWindow:         model.ContextWindow,
			MaxOutputTokens:       model.MaxOutputTokens,
			ReasoningLevels:       model.ReasoningLevels,
			DefaultReasoningLevel: model.DefaultReasoningLevel,
		})
	}
	return document, nil
}

// SaveModelConfig 将 DTO 转为 request，并把用户可修正的配置错误作为正常结果返回。
func (b *ModelBindings) SaveModelConfig(wire dto.SaveModelConfigRequest) (dto.SaveModelConfigResponse, error) {
	candidate := request.SaveModelConfig{
		Groups:            make([]request.ModelGroup, 0, len(wire.Groups)),
		DefaultProviderID: wire.DefaultProviderID,
		Providers:         make([]request.Provider, 0, len(wire.Providers)),
		Models:            make([]request.ConfiguredModel, 0, len(wire.Models)),
	}
	for _, group := range wire.Groups {
		candidate.Groups = append(candidate.Groups, request.ModelGroup{
			ID:          group.ID,
			DisplayName: group.DisplayName,
		})
	}
	for _, provider := range wire.Providers {
		candidate.Providers = append(candidate.Providers, request.Provider{
			ID:             provider.ID,
			DisplayName:    provider.ProviderName,
			BaseURL:        provider.BaseURL,
			ProxyURL:       provider.ProxyURL,
			DefaultModelID: provider.DefaultModelID,
			HasAPIKey:      provider.HasAPIKey,
		})
	}
	for _, model := range wire.Models {
		candidate.Models = append(candidate.Models, request.ConfiguredModel{
			ProviderID:            model.ProviderID,
			ModelID:               model.ModelID,
			DisplayName:           model.Label,
			GroupID:               model.GroupID,
			APIFormat:             model.APIFormat,
			ContextWindow:         model.ContextWindow,
			MaxOutputTokens:       model.MaxOutputTokens,
			ReasoningLevels:       model.ReasoningLevels,
			DefaultReasoningLevel: model.DefaultReasoningLevel,
		})
	}
	canonical, err := request.NewSaveModelConfig(candidate)
	if err != nil {
		return validationResponse(err)
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return dto.SaveModelConfigResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return dto.SaveModelConfigResponse{}, publicError(b.runtime, "ModelBindings.SaveModelConfig.context", err)
	}
	if err := services.ModelConfig.SaveModelConfig(canonical); err != nil {
		var validationError *contracts.ValidationError
		if errors.As(err, &validationError) {
			return dto.SaveModelConfigResponse{ValidationError: validationError.Error()}, nil
		}
		return dto.SaveModelConfigResponse{}, publicError(b.runtime, "ModelBindings.SaveModelConfig", err)
	}
	return dto.SaveModelConfigResponse{Saved: true}, nil
}

func validationResponse(err error) (dto.SaveModelConfigResponse, error) {
	var validationError *contracts.ValidationError
	if errors.As(err, &validationError) {
		return dto.SaveModelConfigResponse{ValidationError: validationError.Error()}, nil
	}
	return dto.SaveModelConfigResponse{}, err
}

// SetProviderKey 保存或清除一个 Provider 的 API key。
func (b *ModelBindings) SetProviderKey(providerID, value string) error {
	if err := validation.ValidateProviderID(providerID); err != nil {
		return err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return publicError(b.runtime, "ModelBindings.SetProviderKey.context", err)
	}
	if err := services.ModelConfig.SetProviderKey(providerID, value); err != nil {
		return publicError(b.runtime, "ModelBindings.SetProviderKey", err)
	}
	return nil
}

// ClearProviderKey 清除一个 Provider 的 API key。
func (b *ModelBindings) ClearProviderKey(providerID string) error {
	return b.SetProviderKey(providerID, "")
}

// ListProviderModels 查询已保存 Provider 的远程模型目录。
func (b *ModelBindings) ListProviderModels(providerID string) ([]string, error) {
	if err := validation.ValidateProviderID(providerID); err != nil {
		return nil, err
	}
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	models, err := services.ModelConfig.DiscoverProviderModels(ctx, providerID)
	if err != nil {
		return nil, publicError(b.runtime, "ModelBindings.ListProviderModels", err)
	}
	return append([]string{}, models...), nil
}

// ReloadConfig 从磁盘重新读取配置；失败时保留当前运行时配置。
func (b *ModelBindings) ReloadConfig() error {
	ctx, services, err := b.runtime.BindingContext()
	if err != nil {
		return err
	}
	if err := services.ModelConfig.ReloadConfig(ctx); err != nil {
		return publicError(b.runtime, "ModelBindings.ReloadConfig", err)
	}
	return nil
}
