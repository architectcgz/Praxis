package modelregistry

import (
	"fmt"
	"net/http"

	providerapi "praxis/internal/providers"
)

// Load reads the persisted model configuration and auth documents, validates
// both, and constructs the runtime registry. The two documents are separate
// facts: models.json never contains a secret, and auth.json is keyed by
// ProviderID.
func Load(modelsPath, credentialsPath string, client *http.Client) (*Registry, error) {
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
	registry, err := newRegistry(modelsPath, credentialsPath, validated, credentials, client)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	return registry, nil
}

func newRegistry(modelsPath, credentialsPath string, validated ValidatedConfig, credentials ProviderCredentials, client *http.Client) (*Registry, error) {
	config := validated.config
	providerClients, err := buildProviderClients(config.Providers, client)
	if err != nil {
		return nil, err
	}
	modelsByKey, providersByID := buildIndexes(config.Providers)
	return &Registry{
		modelsPath: modelsPath, credentialsPath: credentialsPath,
		config: config, credentials: credentials,
		client: client, providerClients: providerClients,
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
