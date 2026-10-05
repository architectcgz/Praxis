package modelregistry

import (
	"errors"
)

// ApplyValidatedConfig 原子持久化已准备的配置并替换内存索引。
func (r *Registry) ApplyValidatedConfig(validated ValidatedConfig) error {
	if r == nil {
		return errors.New("registry is not initialized")
	}
	if !validated.prepared {
		return errors.New("model configuration has not been prepared")
	}
	nextConfig := validated.config
	providerClients, err := buildProviderClients(nextConfig.Providers, r.client)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	nextCredentials := retainCredentialsForProviders(r.credentials, nextConfig.Providers)
	modelsByKey, providersByID := buildIndexes(nextConfig.Providers)
	if credentialsChanged(r.credentials, nextCredentials) && r.credentialsPath != "" {
		if err := writeConfigAtomically(r.credentialsPath, nextCredentials); err != nil {
			return &ConfigurationError{Path: r.credentialsPath, Err: err}
		}
	}
	if err := writeConfigAtomically(r.modelsPath, nextConfig); err != nil {
		return &ConfigurationError{Path: r.modelsPath, Err: err}
	}
	r.config = nextConfig
	r.credentials = nextCredentials
	r.modelsByKey = modelsByKey
	r.providersByID = providersByID
	r.providerClients = providerClients
	return nil
}
