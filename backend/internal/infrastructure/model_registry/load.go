package modelregistry

import (
	"fmt"
	"net/http"

	domainmodel "praxis/internal/domain/model"
	providerapi "praxis/internal/providers"
)

// Load reads the persisted configuration, validates it, and constructs its runtime registry.
func Load(modelsPath string, client *http.Client) (*Registry, error) {
	config, err := readModelConfigFile(modelsPath)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	validated, err := Prepare(config)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	registry, err := newRegistry(modelsPath, validated, client)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	return registry, nil
}

func newRegistry(modelsPath string, validated ValidatedConfig, client *http.Client) (*Registry, error) {
	config := validated.config
	providerClients, err := buildProviderClients(config.Providers, client)
	if err != nil {
		return nil, err
	}
	modelsByKey, providersByID := buildIndexes(config.Providers, validated.models)
	return &Registry{
		modelsPath: modelsPath,
		config:     config, client: client, providerClients: providerClients,
		modelsByKey: modelsByKey, providersByID: providersByID,
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

func buildIndexes(providers []ProviderConfig, domainModels map[modelKey]domainmodel.Model) (map[modelKey]registeredModel, map[string]ProviderConfig) {
	modelsByKey := make(map[modelKey]registeredModel)
	providersByID := make(map[string]ProviderConfig, len(providers))
	for _, provider := range providers {
		providersByID[provider.ID] = provider
		for _, model := range provider.Models {
			key := modelKey{provider.ID, model.ID}
			modelsByKey[key] = registeredModel{config: model, domain: domainModels[key]}
		}
	}
	return modelsByKey, providersByID
}
