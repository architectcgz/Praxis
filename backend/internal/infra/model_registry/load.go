package modelregistry

import (
	"fmt"
	"net/http"
	providerapi "praxis/internal/infra/providers"
)

// Load 读取并校验模型配置与凭证文件，然后构造运行时注册表。
// 可选的 StreamFactory 负责在组合根创建具体协议适配器。
func Load(modelsPath, credentialsPath string, client *http.Client, factories ...StreamFactory) (*Registry, error) {
	var streamFactory StreamFactory
	if len(factories) > 0 {
		streamFactory = factories[0]
	}
	config, err := readModelConfigFile(modelsPath)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	validated, err := Prepare(config)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	credentials, err := readCredentialsFile(credentialsPath)
	if err != nil {
		return nil, &ConfigurationError{Path: credentialsPath, Err: err}
	}
	credentials, err = normalizeCredentials(credentials)
	if err != nil {
		return nil, &ConfigurationError{Path: credentialsPath, Err: err}
	}
	registry, err := newRegistry(modelsPath, credentialsPath, validated, credentials, client, streamFactory)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	return registry, nil
}

func newRegistry(
	modelsPath string,
	credentialsPath string,
	validated ValidatedConfig,
	credentials ProviderCredentials,
	client *http.Client,
	streamFactory StreamFactory,
) (*Registry, error) {
	config := validated.config
	providerClients, err := buildProviderClients(config.Providers, client)
	if err != nil {
		return nil, err
	}
	modelsByKey, providersByID := buildIndexes(config.Providers)
	return &Registry{
		modelsPath:      modelsPath,
		credentialsPath: credentialsPath,
		config:          config,
		credentials:     credentials,
		client:          client,
		streamFactory:   streamFactory,
		providerClients: providerClients,
		modelsByKey:     modelsByKey,
		providersByID:   providersByID,
	}, nil
}

func buildProviderClients(providers []ProviderConfig, base *http.Client) (map[string]*http.Client, error) {
	clients := make(map[string]*http.Client, len(providers))
	for _, provider := range providers {
		client, err := providerapi.NewProxyClient(base, provider.ProxyURL)
		if err != nil {
			return nil, fmt.Errorf("provider %q: %w", provider.ID, err)
		}
		clients[provider.ID] = client
	}
	return clients, nil
}

func buildIndexes(providers []ProviderConfig) (map[modelKey]ModelConfig, map[string]ProviderConfig) {
	modelsByKey := make(map[modelKey]ModelConfig)
	providersByID := make(map[string]ProviderConfig, len(providers))
	for _, provider := range providers {
		providersByID[provider.ID] = provider
		for _, model := range provider.Models {
			modelsByKey[modelKey{provider.ID, model.ID}] = model
		}
	}
	return modelsByKey, providersByID
}
