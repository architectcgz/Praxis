package bindings

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"praxis/internal/fault"
	modelconfig "praxis/internal/model/config"
	"praxis/wails/dto"
)

// modelConfigFromRequest 在 Wails 输入边界复制、规范化并校验配置；回显的密钥状态不参与保存。
func modelConfigFromRequest(value dto.SaveModelConfigRequest) (modelconfig.Config, error) {
	config := modelconfig.Config{
		Groups:            make([]modelconfig.Group, 0, len(value.Groups)),
		DefaultProviderID: strings.TrimSpace(value.DefaultProviderID),
		Providers:         make([]modelconfig.Provider, 0, len(value.Providers)),
	}
	for _, group := range value.Groups {
		config.Groups = append(config.Groups, modelconfig.Group{
			ID:          strings.TrimSpace(group.ID),
			DisplayName: strings.TrimSpace(group.DisplayName),
		})
	}
	for _, provider := range value.Providers {
		providerID := strings.TrimSpace(provider.ID)
		config.Providers = append(config.Providers, modelconfig.Provider{
			ID:             providerID,
			DisplayName:    cmp.Or(strings.TrimSpace(provider.ProviderName), providerID),
			BaseURL:        strings.TrimRight(strings.TrimSpace(provider.BaseURL), "/"),
			ProxyURL:       strings.TrimRight(strings.TrimSpace(provider.ProxyURL), "/"),
			DefaultModelID: strings.TrimSpace(provider.DefaultModelID),
		})
	}
	providerIDs := make(map[string]struct{}, len(config.Providers))
	for _, provider := range config.Providers {
		providerIDs[provider.ID] = struct{}{}
	}
	for index, model := range value.Models {
		providerID := strings.TrimSpace(model.ProviderID)
		modelID := strings.TrimSpace(model.ModelID)
		if providerID == "" {
			return modelconfig.Config{}, fault.InvalidValue(fmt.Sprintf("models[%d].providerId", index), "required")
		}
		if _, ok := providerIDs[providerID]; !ok {
			return modelconfig.Config{}, fault.InvalidValue(fmt.Sprintf("models[%d].providerId", index), "unknown provider")
		}
		levels := slices.Clone(model.ReasoningLevels)
		for levelIndex := range levels {
			levels[levelIndex] = strings.TrimSpace(levels[levelIndex])
		}
		configured := modelconfig.Model{
			ID:                    modelID,
			DisplayName:           cmp.Or(strings.TrimSpace(model.Label), modelID),
			GroupID:               strings.TrimSpace(model.GroupID),
			APIFormat:             modelconfig.APIFormat(strings.TrimSpace(model.APIFormat)),
			ContextWindow:         model.ContextWindow,
			MaxOutputTokens:       model.MaxOutputTokens,
			ReasoningLevels:       levels,
			DefaultReasoningLevel: strings.TrimSpace(model.DefaultReasoningLevel),
			Thinking: modelconfig.ModelThinking{
				Mode:             strings.TrimSpace(model.Thinking.Mode),
				BudgetTokens:     model.Thinking.BudgetTokens,
				SupportsDisabled: model.Thinking.SupportsDisabled,
			},
		}
		for providerIndex := range config.Providers {
			if config.Providers[providerIndex].ID == providerID {
				config.Providers[providerIndex].Models = append(config.Providers[providerIndex].Models, configured)
				break
			}
		}
	}
	if err := modelconfig.Validate(config); err != nil {
		return modelconfig.Config{}, fault.InvalidValue("modelConfig", err.Error())
	}
	return config, nil
}
