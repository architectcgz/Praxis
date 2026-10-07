package request

import (
	"cmp"
	"fmt"
	"slices"
	"strings"

	"praxis/internal/contracts"
	modelconfig "praxis/internal/core/model/config"
)

// ModelGroup 是配置请求中的分组数据。
type ModelGroup struct {
	ID          string
	DisplayName string
}

// Provider 是配置请求中的 Provider 数据；HasAPIKey 只用于回显，不参与保存。
type Provider struct {
	ID             string
	DisplayName    string
	BaseURL        string
	ProxyURL       string
	DefaultModelID string
	HasAPIKey      bool
}

// ConfiguredModel 是配置请求中的模型能力数据。
type ConfiguredModel struct {
	ProviderID            string
	ModelID               string
	DisplayName           string
	GroupID               string
	APIFormat             string
	ContextWindow         int
	MaxOutputTokens       int
	ReasoningLevels       []string
	DefaultReasoningLevel string
}

// SaveModelConfig 是已准备好的模型配置请求。
type SaveModelConfig struct {
	Groups            []ModelGroup
	DefaultProviderID string
	Providers         []Provider
	Models            []ConfiguredModel
	validated         modelconfig.ValidatedConfig
}

// NewSaveModelConfig 复制、规范化并校验完整模型配置，拒绝丢失 Provider 引用的模型。
func NewSaveModelConfig(value SaveModelConfig) (SaveModelConfig, error) {
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
			DisplayName:    cmp.Or(strings.TrimSpace(provider.DisplayName), providerID),
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
		if providerID == "" {
			return SaveModelConfig{}, contracts.InvalidValue(fmt.Sprintf("models[%d].providerId", index), "required")
		}
		if _, ok := providerIDs[providerID]; !ok {
			return SaveModelConfig{}, contracts.InvalidValue(fmt.Sprintf("models[%d].providerId", index), "unknown provider")
		}
		levels := slices.Clone(model.ReasoningLevels)
		for levelIndex := range levels {
			levels[levelIndex] = strings.TrimSpace(levels[levelIndex])
		}
		configured := modelconfig.Model{
			ID:                    strings.TrimSpace(model.ModelID),
			DisplayName:           cmp.Or(strings.TrimSpace(model.DisplayName), strings.TrimSpace(model.ModelID)),
			GroupID:               strings.TrimSpace(model.GroupID),
			APIFormat:             modelconfig.APIFormat(strings.TrimSpace(model.APIFormat)),
			ContextWindow:         model.ContextWindow,
			MaxOutputTokens:       model.MaxOutputTokens,
			ReasoningLevels:       levels,
			DefaultReasoningLevel: strings.TrimSpace(model.DefaultReasoningLevel),
		}
		for providerIndex := range config.Providers {
			if config.Providers[providerIndex].ID == providerID {
				config.Providers[providerIndex].Models = append(config.Providers[providerIndex].Models, configured)
				break
			}
		}
	}
	validated, err := modelconfig.NewValidatedConfig(config)
	if err != nil {
		return SaveModelConfig{}, contracts.InvalidValue("modelConfig", err.Error())
	}
	value.validated = validated
	return value, nil
}

// ValidatedConfig 返回不可变配置，供 service 交给 registry 保存。
func (value SaveModelConfig) ValidatedConfig() modelconfig.ValidatedConfig {
	return value.validated
}

// Config 返回 canonical 配置副本，供 service 校验 Agent 模型引用。
func (value SaveModelConfig) Config() modelconfig.Config {
	return value.validated.Config()
}
