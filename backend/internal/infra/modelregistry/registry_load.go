package modelregistry

import (
	"fmt"
	"net/http"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/infra/providers/httpclient"
	"slices"
)

// Load 读取并校验模型配置与凭证文件，然后构造运行时注册表。
func Load(modelsPath, credentialsPath string, client *http.Client) (*Registry, error) {
	config, err := readModelConfigFile(modelsPath)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	credentials, err := readCredentialsFile(credentialsPath)
	if err != nil {
		return nil, &ConfigurationError{Path: credentialsPath, Err: err}
	}
	credentials, err = validateCredentials(credentials)
	if err != nil {
		return nil, &ConfigurationError{Path: credentialsPath, Err: err}
	}
	registry, err := newRegistry(modelsPath, credentialsPath, config, credentials, client)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	return registry, nil
}

func newRegistry(
	modelsPath string,
	credentialsPath string,
	config modelconfig.Config,
	credentials ProviderCredentials,
	client *http.Client,
) (*Registry, error) {
	providerClients, err := buildProviderClients(config.Providers, client)
	if err != nil {
		return nil, err
	}
	providersByID, modelConfigs := buildIndexes(config.Providers)
	return &Registry{
		modelsPath:      modelsPath,
		credentialsPath: credentialsPath,
		config:          config,
		credentials:     credentials,
		client:          client,
		providerClients: providerClients,
		modelConfigs:    modelConfigs,
		providersByID:   providersByID,
	}, nil
}

func buildProviderClients(providers []modelconfig.Provider, base *http.Client) (map[string]*http.Client, error) {
	clients := make(map[string]*http.Client, len(providers))
	for _, provider := range providers {
		client, err := httpclient.NewProxyClient(base, provider.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", provider.ID, err)
		}
		clients[provider.ID] = client
	}
	return clients, nil
}

// buildIndexes 在配置发布前建立查询索引和完整的执行配置，运行期不再逐字段组装。
func buildIndexes(providers []modelconfig.Provider) (map[string]modelconfig.Provider, map[modelKey]modelapi.Config) {
	providersByID := make(map[string]modelconfig.Provider, len(providers))
	modelConfigs := make(map[modelKey]modelapi.Config)
	for _, provider := range providers {
		providersByID[provider.ID] = provider
		for _, model := range provider.Models {
			key := modelKey{ProviderID: provider.ID, ModelID: model.ID}
			modelConfigs[key] = modelapi.Config{
				ProviderID:      provider.ID,
				ModelID:         model.ID,
				ReasoningLevel:  model.DefaultReasoningLevel,
				ReasoningLevels: slices.Clone(model.ReasoningLevels),
				Thinking:        model.Thinking,
				APIFormat:       string(model.APIFormat),
				ContextWindow:   model.ContextWindow,
				MaxOutputTokens: model.MaxOutputTokens,
				BaseURL:         provider.BaseURL,
				ProxyURL:        provider.ProxyURL,
			}
		}
	}
	return providersByID, modelConfigs
}
