package modelregistry

import (
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"

	"fmt"
	providerhttp "praxis/internal/infra/providers"
)

// BuildModel 根据 Task 的模型配置构建运行时模型。
// 这里不能重新读取当前模型配置，否则配置热更新会改变执行中的请求行为。
func (r *Registry) BuildModel(config model.Config) (model.Model, error) {
	if r == nil {
		return model.Model{}, fmt.Errorf("model registry is not initialized")
	}
	if err := config.Validate(); err != nil {
		return model.Model{}, err
	}
	baseURL, err := modelconfig.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return model.Model{}, err
	}
	proxyURL, err := modelconfig.ValidateProxyURL(config.ProxyURL)
	if err != nil {
		return model.Model{}, err
	}
	r.mu.RLock()
	apiKey := r.credentialKeyLocked(config.ProviderID)
	client := r.client
	streamFactory := r.streamFactory
	r.mu.RUnlock()
	if apiKey == "" {
		return model.Model{}, fmt.Errorf("API key is not configured for provider %q", config.ProviderID)
	}
	if streamFactory == nil {
		return model.Model{}, fmt.Errorf("model stream factory is not configured")
	}
	client, err = providerhttp.NewProxyClient(client, proxyURL)
	if err != nil {
		return model.Model{}, fmt.Errorf("create provider client: %w", err)
	}
	stream, err := streamFactory(
		modelconfig.APIFormat(config.APIFormat),
		modelconfig.Provider{
			ID:       config.ProviderID,
			BaseURL:  baseURL,
			ProxyURL: proxyURL,
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
		return model.Model{}, err
	}
	return model.Model{
		Stream:          stream,
		MaxOutputTokens: config.MaxOutputTokens,
	}, nil
}
