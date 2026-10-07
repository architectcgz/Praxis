package validation

import (
	"cmp"
	"slices"
	"strings"

	modelconfig "praxis/internal/core/model/config"
	"praxis/wails/dto"
)

// PrepareModelConfig 在 Wails 配置请求入口规范化字段、校验引用并转换为业务配置。
// 不修改请求原值；引用错误返回输入校验错误，配置错误返回 ValidationError。
func PrepareModelConfig(request dto.SaveModelConfigRequest) (modelconfig.ValidatedConfig, error) {
	request.Providers = slices.Clone(request.Providers)
	request.Models = slices.Clone(request.Models)
	for index := range request.Providers {
		request.Providers[index].ID = strings.TrimSpace(request.Providers[index].ID)
	}
	for index := range request.Models {
		request.Models[index].ProviderID = strings.TrimSpace(request.Models[index].ProviderID)
	}
	if err := ValidateSaveModelConfig(request); err != nil {
		return modelconfig.ValidatedConfig{}, err
	}
	config := modelconfig.Config{
		Groups:            make([]modelconfig.Group, 0, len(request.Groups)),
		DefaultProviderID: strings.TrimSpace(request.DefaultProviderID),
		Providers:         make([]modelconfig.Provider, 0, len(request.Providers)),
	}
	for _, group := range request.Groups {
		config.Groups = append(config.Groups, modelconfig.Group{
			ID:          strings.TrimSpace(group.ID),
			DisplayName: strings.TrimSpace(group.DisplayName),
		})
	}
	for _, provider := range request.Providers {
		config.Providers = append(config.Providers, modelconfig.Provider{
			ID:             provider.ID,
			DisplayName:    cmp.Or(strings.TrimSpace(provider.ProviderName), provider.ID),
			BaseURL:        strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/"),
			ProxyURL:       strings.TrimRight(strings.TrimSpace(provider.ProxyURL), "/"),
			DefaultModelID: strings.TrimSpace(provider.DefaultModelID),
		})
	}
	for _, option := range request.Models {
		modelID := strings.TrimSpace(option.ModelID)
		configured := modelconfig.Model{
			ID:                    modelID,
			DisplayName:           cmp.Or(strings.TrimSpace(option.Label), modelID),
			GroupID:               strings.TrimSpace(option.GroupID),
			APIFormat:             modelconfig.APIFormat(strings.TrimSpace(option.APIFormat)),
			ContextWindow:         option.ContextWindow,
			MaxOutputTokens:       option.MaxOutputTokens,
			ReasoningLevels:       slices.Clone(option.ReasoningLevels),
			DefaultReasoningLevel: strings.TrimSpace(option.DefaultReasoningLevel),
		}
		for index, level := range configured.ReasoningLevels {
			configured.ReasoningLevels[index] = strings.TrimSpace(level)
		}
		for index := range config.Providers {
			if config.Providers[index].ID == option.ProviderID {
				config.Providers[index].Models = append(config.Providers[index].Models, configured)
				break
			}
		}
	}
	prepared, err := modelconfig.NewValidatedConfig(config)
	if err != nil {
		return modelconfig.ValidatedConfig{}, &modelconfig.ValidationError{Err: err}
	}
	return prepared, nil
}
