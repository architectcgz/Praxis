// Package model 提供模型目录与配置的应用入口和安全展示投影。
package model

import (
	"context"
	"slices"
	"sync"

	modelcontracts "praxis/internal/app/model/contracts"
	"praxis/internal/request"
	businessmodel "praxis/internal/service/model"
)

// App 适配模型配置业务；配置锁与会话创建和输入提交共用。
type App struct {
	models   *businessmodel.Service
	configMu *sync.RWMutex
}

// New 注入模型业务和唯一配置锁，不创建独立配置快照。
func New(models *businessmodel.Service, configMu *sync.RWMutex) *App {
	return &App{models: models, configMu: configMu}
}

// ListModels 返回可展示的模型目录，不暴露内部目录对象。
func (a *App) ListModels() []modelcontracts.ModelOption {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	models := a.models.ListModels()
	result := make([]modelcontracts.ModelOption, 0, len(models))
	for _, model := range models {
		result = append(result, modelcontracts.ModelOption{
			ProviderID:               model.ProviderID,
			ModelID:                  model.ModelID,
			DefaultProviderID:        model.DefaultProviderID,
			DefaultModelID:           model.DefaultModelID,
			Label:                    model.Label,
			ProviderName:             model.ProviderName,
			ReasoningLevels:          slices.Clone(model.ReasoningLevels),
			DefaultReasoningLevel:    model.DefaultReasoningLevel,
			AssignedAgentDefinitions: model.AssignedAgentDefinitions,
		})
	}
	return result
}

// ModelConfig 返回可编辑配置和凭据存在状态，不包含凭据内容。
func (a *App) ModelConfig() modelcontracts.ModelConfig {
	a.configMu.RLock()
	defer a.configMu.RUnlock()
	config := a.models.ModelConfig()
	result := modelcontracts.ModelConfig{
		Groups:            make([]modelcontracts.ModelConfigGroup, 0, len(config.Groups)),
		DefaultProviderID: config.DefaultProviderID,
		Providers:         make([]modelcontracts.ModelConfigProvider, 0, len(config.Providers)),
		Models:            []modelcontracts.ConfiguredModel{},
	}
	for _, group := range config.Groups {
		result.Groups = append(result.Groups, modelcontracts.ModelConfigGroup{
			ID:          group.ID,
			DisplayName: group.DisplayName,
		})
	}
	for _, provider := range config.Providers {
		result.Providers = append(result.Providers, modelcontracts.ModelConfigProvider{
			ID:             provider.ID,
			DisplayName:    provider.DisplayName,
			BaseURL:        provider.BaseURL,
			ProxyURL:       provider.ProxyURL,
			DefaultModelID: provider.DefaultModelID,
			HasAPIKey:      a.models.HasProviderKey(provider.ID),
		})
		for _, model := range provider.Models {
			result.Models = append(result.Models, modelcontracts.ConfiguredModel{
				ProviderID:            provider.ID,
				ModelID:               model.ID,
				DisplayName:           model.DisplayName,
				GroupID:               model.GroupID,
				APIFormat:             string(model.APIFormat),
				ContextWindow:         model.ContextWindow,
				MaxOutputTokens:       model.MaxOutputTokens,
				ReasoningLevels:       slices.Clone(model.ReasoningLevels),
				DefaultReasoningLevel: model.DefaultReasoningLevel,
				Thinking: modelcontracts.ModelThinking{
					Mode:             model.Thinking.Mode,
					BudgetTokens:     model.Thinking.BudgetTokens,
					SupportsDisabled: model.Thinking.SupportsDisabled,
				},
			})
		}
	}
	return result
}

// SaveModelConfig 接收 canonical 配置；业务引用约束和保存由模型业务负责。
func (a *App) SaveModelConfig(canonical request.SaveModelConfig) error {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	return a.models.SaveModelConfig(canonical.Config())
}

// SetProviderKey 保存 canonical 凭据请求，不缓存或返回密钥内容。
func (a *App) SetProviderKey(canonical request.SetProviderKey) error {
	a.configMu.Lock()
	defer a.configMu.Unlock()
	return a.models.SetProviderKey(canonical.ProviderID, canonical.Key)
}

// DiscoverProviderModels 查询已配置 Provider 的模型目录，取消由调用方控制。
func (a *App) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
	return a.models.DiscoverProviderModels(ctx, providerID)
}

// ReloadConfig 串行重载全部配置，失败时保留当前运行时快照。
func (a *App) ReloadConfig(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	a.configMu.Lock()
	defer a.configMu.Unlock()
	return a.models.ReloadConfig(ctx)
}
