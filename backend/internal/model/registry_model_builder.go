package model

import (
	"fmt"
	modelconfig "praxis/internal/model/config"
)

// BuildModel 根据 Task 的模型配置构建运行时模型。
// 这里不能重新读取当前模型配置，否则配置热更新会改变执行中的请求行为。
func (r *Registry) BuildModel(config Config) (Model, error) {
	if r == nil {
		return Model{}, fmt.Errorf("model registry is not initialized")
	}
	r.mu.RLock()
	apiKey := r.credentialKeyLocked(config.ProviderID)
	client := r.client
	streamFactory := r.streamFactory
	r.mu.RUnlock()
	if apiKey == "" {
		return Model{}, fmt.Errorf("API key is not configured for provider %q", config.ProviderID)
	}
	if streamFactory == nil {
		return Model{}, fmt.Errorf("model stream factory is not configured")
	}
	client, err := NewProxyClient(client, config.ProxyURL)
	if err != nil {
		return Model{}, fmt.Errorf("create provider client: %w", err)
	}
	stream, err := streamFactory(
		modelconfig.APIFormat(config.APIFormat),
		modelconfig.Provider{
			ID:       config.ProviderID,
			BaseURL:  config.BaseURL,
			ProxyURL: config.ProxyURL,
		},
		modelconfig.Model{
			ID:              config.ModelID,
			APIFormat:       modelconfig.APIFormat(config.APIFormat),
			ContextWindow:   config.ContextWindow,
			MaxOutputTokens: config.MaxOutputTokens,
			ReasoningLevels: config.Clone().ReasoningLevels,
			Thinking:        config.Thinking,
		},
		apiKey, client,
	)
	if err != nil {
		return Model{}, err
	}
	return Model{
		Stream:          stream,
		MaxOutputTokens: config.MaxOutputTokens,
	}, nil
}
