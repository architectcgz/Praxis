package modelregistry

import (
	"praxis/internal/contracts"

	"fmt"
	providerhttp "praxis/internal/infra/providers"

	runtimecontract "praxis/internal/agent_runtime"
)

// BuildTurnModel 根据 turn 创建时冻结的快照构建运行时模型。
// 这里不能重新读取当前模型配置，否则配置热更新会改变执行中的请求行为。
func (r *Registry) BuildTurnModel(snapshot contracts.ModelSnapshot) (runtimecontract.TurnModel, error) {
	if r == nil {
		return runtimecontract.TurnModel{}, fmt.Errorf("model registry is not initialized")
	}
	if err := snapshot.Validate(); err != nil {
		return runtimecontract.TurnModel{}, err
	}
	baseURL, err := providerhttp.ValidateBaseURL(snapshot.BaseURL)
	if err != nil {
		return runtimecontract.TurnModel{}, err
	}
	proxyURL, err := providerhttp.ValidateProxyURL(snapshot.ProxyURL)
	if err != nil {
		return runtimecontract.TurnModel{}, err
	}
	r.mu.RLock()
	apiKey := r.credentialKeyLocked(snapshot.ProviderID)
	client := r.client
	streamFactory := r.streamFactory
	r.mu.RUnlock()
	if apiKey == "" {
		return runtimecontract.TurnModel{}, fmt.Errorf("API key is not configured for provider %q", snapshot.ProviderID)
	}
	if streamFactory == nil {
		return runtimecontract.TurnModel{}, fmt.Errorf("model stream factory is not configured")
	}
	client, err = providerhttp.NewProxyClient(client, proxyURL)
	if err != nil {
		return runtimecontract.TurnModel{}, fmt.Errorf("create provider client: %w", err)
	}
	stream, err := streamFactory(
		ModelAPIFormat(snapshot.APIFormat),
		ProviderConfig{ID: snapshot.ProviderID, BaseURL: baseURL, ProxyURL: proxyURL},
		ModelConfig{
			ID: snapshot.ModelID, APIFormat: ModelAPIFormat(snapshot.APIFormat),
			ContextWindow: snapshot.ContextWindow, MaxOutputTokens: snapshot.MaxOutputTokens,
		},
		apiKey, client,
	)
	if err != nil {
		return runtimecontract.TurnModel{}, err
	}
	return runtimecontract.TurnModel{
		Stream: stream, MaxOutputTokens: snapshot.MaxOutputTokens,
	}, nil
}
