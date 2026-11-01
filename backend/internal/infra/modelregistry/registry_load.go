package modelregistry

import (
	"fmt"
	"net/http"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/infra/providers/httpclient"
)

// Load 读取并校验模型配置与凭据文件，然后构造管理端注册表。
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
	return &Registry{
		modelsPath:      modelsPath,
		credentialsPath: credentialsPath,
		config:          config,
		credentials:     credentials,
		client:          client,
		providerClients: providerClients,
		providersByID:   indexProviders(config.Providers),
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

func indexProviders(providers []modelconfig.Provider) map[string]modelconfig.Provider {
	indexed := make(map[string]modelconfig.Provider, len(providers))
	for _, provider := range providers {
		indexed[provider.ID] = provider
	}
	return indexed
}
