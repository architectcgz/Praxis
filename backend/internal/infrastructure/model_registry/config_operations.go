package modelregistry

import (
	"errors"
)

// ApplyConfig validates and atomically writes the model configuration, then
// replaces the in-memory indexes in place. The runtime model resolver and the
// bindings share this one *Registry, so an in-place
// swap makes a save effective without restarting the process. A validation
// failure writes nothing and leaves memory untouched.
func (r *Registry) ApplyConfig(config RegistryConfig) error {
	validated, err := Prepare(config)
	if err != nil {
		return err
	}
	return r.ApplyValidatedConfig(validated)
}

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
