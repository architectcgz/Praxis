package bindings

import (
	"context"
	"errors"
	"fmt"
	"praxis/internal/errorcode"
	apperr "praxis/wails/error"
	"regexp"
	"strings"
	"time"

	"praxis/wails/dto"
	"praxis/wails/validation"
)

// GetModelConfig 返回可编辑模型元数据和密钥存在状态，不返回密钥内容。
func (b *ModelBindings) GetModelConfig() (dto.ModelConfigDocument, error) {
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.ModelConfigDocument{}, err
	}
	if err := ctx.Err(); err != nil {
		return dto.ModelConfigDocument{}, publicError(b.host, "ModelBindings.GetModelConfig.context", err)
	}
	snapshot := services.ModelConfig.Configuration()
	config := snapshot.Config
	document := dto.ModelConfigDocument{
		Groups:    make([]dto.GroupConfigOption, 0, len(config.Groups)),
		Providers: make([]dto.ProviderConfigOption, 0, len(config.Providers)),
		Models:    []dto.ModelConfigOption{},
	}
	for _, group := range config.Groups {
		document.Groups = append(document.Groups, dto.GroupConfigOption{
			ID:          group.ID,
			DisplayName: group.DisplayName,
		})
	}
	for _, provider := range config.Providers {
		document.Providers = append(document.Providers, dto.ProviderConfigOption{
			ID:           provider.ID,
			ProviderName: provider.DisplayName,
			BaseURL:      provider.BaseURL,
			ProxyURL:     provider.ProxyURL,
			HasAPIKey:    snapshot.ProviderKeys[provider.ID],
		})
		for _, configured := range provider.Models {
			document.Models = append(document.Models, dto.ModelConfigOption{
				ProviderID:            provider.ID,
				ModelID:               configured.ID,
				Label:                 configured.DisplayName,
				GroupID:               configured.GroupID,
				APIFormat:             string(configured.APIFormat),
				ContextWindow:         configured.ContextWindow,
				MaxOutputTokens:       configured.MaxOutputTokens,
				ReasoningLevels:       configured.ReasoningLevels,
				DefaultReasoningLevel: configured.DefaultReasoningLevel,
				Thinking:              dto.ModelThinking(configured.Thinking),
			})
		}
	}
	return document, nil
}

// SaveModelConfig 在模型输入边界规范化配置，并把可修正的字段错误作为正常结果返回。
func (b *ModelBindings) SaveModelConfig(wire dto.SaveModelConfigRequest) (dto.SaveModelConfigResponse, error) {
	canonical, err := modelConfigFromRequest(wire)
	if err != nil {
		return validationResponse(err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return dto.SaveModelConfigResponse{}, err
	}
	if err := ctx.Err(); err != nil {
		return dto.SaveModelConfigResponse{}, publicError(b.host, "ModelBindings.SaveModelConfig.context", err)
	}
	if err := services.ModelConfig.SaveModelConfig(canonical); err != nil {
		if errors.Is(err, errorcode.ErrInvalidValue) {
			return validationResponse(err)
		}
		return dto.SaveModelConfigResponse{}, publicError(b.host, "ModelBindings.SaveModelConfig", err)
	}
	return dto.SaveModelConfigResponse{Saved: true}, nil
}

func validationResponse(err error) (dto.SaveModelConfigResponse, error) {
	if errors.Is(err, errorcode.ErrInvalidValue) {
		return dto.SaveModelConfigResponse{ValidationError: apperr.InputMessage(err)}, nil
	}
	return dto.SaveModelConfigResponse{}, apperr.PublicError(err)
}

var providerKeyIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// SetProviderKey 在输入边界校验 Provider 身份；密钥原样保存，空字符串表示清除。
func (b *ModelBindings) SetProviderKey(providerID, value string) error {
	providerID = strings.TrimSpace(providerID)
	if !providerKeyIDPattern.MatchString(providerID) {
		return publicInputError(b.host, "ModelBindings.SetProviderKey.request", fmt.Errorf("providerId: invalid provider id: %w", errorcode.ErrInvalidValue))
	}
	if value != "" && strings.TrimSpace(value) == "" {
		return publicInputError(b.host, "ModelBindings.SetProviderKey.request", fmt.Errorf("key: API key cannot contain only whitespace: %w", errorcode.ErrInvalidValue))
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return publicError(b.host, "ModelBindings.SetProviderKey.context", err)
	}
	if err := services.ModelConfig.SetProviderKey(providerID, value); err != nil {
		return publicError(b.host, "ModelBindings.SetProviderKey", err)
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
		return nil, publicInputError(b.host, "ModelBindings.ListProviderModels.request", err)
	}
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	models, err := services.ModelConfig.DiscoverProviderModels(ctx, providerID)
	if err != nil {
		return nil, publicError(b.host, "ModelBindings.ListProviderModels", err)
	}
	return append([]string{}, models...), nil
}

// ReloadConfig 从磁盘重新读取配置；失败时保留当前运行时配置。
func (b *ModelBindings) ReloadConfig() error {
	ctx, services, err := b.host.BindingContext()
	if err != nil {
		return err
	}
	if err := services.ModelConfig.ReloadConfig(ctx); err != nil {
		return publicError(b.host, "ModelBindings.ReloadConfig", err)
	}
	return nil
}
