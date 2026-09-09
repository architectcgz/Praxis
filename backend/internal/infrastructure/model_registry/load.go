package modelregistry

import (
	"fmt"
	"net/http"

	providerapi "praxis/internal/providers"
)

// Load reads the persisted configuration, validates it, and constructs its runtime registry.
func Load(modelsPath string, client *http.Client) (*Registry, error) {
	config, err := readFileConfig(modelsPath)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	config, err = Validate(config)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	registry, err := newRegistry(modelsPath, config, client)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	return registry, nil
}

func newRegistry(modelsPath string, config FileConfig, client *http.Client) (*Registry, error) {
	providerClients, err := buildProviderClients(config.Providers, client)
	if err != nil {
		return nil, err
	}
	byModel, byProvider := buildIndexes(config.Providers)
	return &Registry{
		modelsPath: modelsPath,
		config:     config, client: client, providerClients: providerClients,
		byModel: byModel, byProvider: byProvider,
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
	byModel := make(map[modelKey]ModelConfig)
	byProvider := make(map[string]ProviderConfig, len(providers))
	for _, provider := range providers {
		byProvider[provider.ID] = provider
		for _, model := range provider.Models {
			byModel[modelKey{provider.ID, model.ID}] = model
		}
	}
	return byModel, byProvider
}
