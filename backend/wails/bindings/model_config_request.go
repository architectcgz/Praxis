package bindings

import (
	"cmp"
	"fmt"
	"strings"

	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/errorcode"
	"praxis/wails/dto"
	"praxis/wails/validation"
)

// modelConfigFromRequest 在 Wails 输入边界规范化并校验配置；回显的密钥状态不参与保存。
func modelConfigFromRequest(value dto.SaveModelConfigRequest) (modelconfig.Config, error) {
	normalizeModelConfigRequest(value)
	if err := validation.Struct(value); err != nil {
		return modelconfig.Config{}, err
	}
	config := modelconfig.Config{
		Groups:    make([]modelconfig.Group, 0, len(value.Groups)),
		Providers: make([]modelconfig.Provider, 0, len(value.Providers)),
	}
	for _, group := range value.Groups {
		config.Groups = append(config.Groups, modelconfig.Group{
			ID:          group.ID,
			DisplayName: group.DisplayName,
		})
	}
	providerIndexes := make(map[string]int, len(value.Providers))
	for index, provider := range value.Providers {
		providerIndexes[provider.ID] = index
		config.Providers = append(config.Providers, modelconfig.Provider{
			ID:          provider.ID,
			DisplayName: provider.ProviderName,
			BaseURL:     provider.BaseURL,
			ProxyURL:    provider.ProxyURL,
		})
	}
	for index, model := range value.Models {
		providerIndex, ok := providerIndexes[model.ProviderID]
		if !ok {
			return modelconfig.Config{}, fmt.Errorf("models[%d].providerId: unknown provider: %w", index, errorcode.ErrInvalidValue)
		}
		configured := modelconfig.Model{
			ID:                    model.ModelID,
			DisplayName:           model.Label,
			GroupID:               model.GroupID,
			APIFormat:             modelconfig.APIFormat(model.APIFormat),
			ContextWindow:         model.ContextWindow,
			MaxOutputTokens:       model.MaxOutputTokens,
			ReasoningLevels:       model.ReasoningLevels,
			DefaultReasoningLevel: model.DefaultReasoningLevel,
			Thinking:              modelconfig.ModelThinking(model.Thinking),
		}
		config.Providers[providerIndex].Models = append(config.Providers[providerIndex].Models, configured)
	}
	// 引用、唯一性与模型能力组合仍使用所属模块的完整规则；配置文件入口复用同一规则。
	if err := modelconfig.Validate(config); err != nil {
		return modelconfig.Config{}, fmt.Errorf("modelConfig: %v: %w", err, errorcode.ErrInvalidValue)
	}
	return config, nil
}

// normalizeModelConfigRequest 就地规范化单次请求的切片数据，调用方不再复用原始输入。
func normalizeModelConfigRequest(value dto.SaveModelConfigRequest) {
	for index := range value.Groups {
		group := &value.Groups[index]
		group.ID = strings.TrimSpace(group.ID)
		group.DisplayName = strings.TrimSpace(group.DisplayName)
	}
	for index := range value.Providers {
		provider := &value.Providers[index]
		provider.ID = strings.TrimSpace(provider.ID)
		provider.ProviderName = cmp.Or(strings.TrimSpace(provider.ProviderName), provider.ID)
		provider.BaseURL = strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/")
		provider.ProxyURL = strings.TrimRight(strings.TrimSpace(provider.ProxyURL), "/")
	}
	for index := range value.Models {
		model := &value.Models[index]
		model.ProviderID = strings.TrimSpace(model.ProviderID)
		model.ModelID = strings.TrimSpace(model.ModelID)
		model.Label = cmp.Or(strings.TrimSpace(model.Label), model.ModelID)
		model.GroupID = strings.TrimSpace(model.GroupID)
		model.APIFormat = strings.TrimSpace(model.APIFormat)
		model.DefaultReasoningLevel = strings.TrimSpace(model.DefaultReasoningLevel)
		model.Thinking.Mode = strings.TrimSpace(model.Thinking.Mode)
		for levelIndex := range model.ReasoningLevels {
			model.ReasoningLevels[levelIndex] = strings.TrimSpace(model.ReasoningLevels[levelIndex])
		}
	}
}
