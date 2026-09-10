package modelregistry

import (
	"errors"
	"fmt"
	"strings"
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
	config := validated.config
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range config.Providers {
		if config.Providers[index].Credential != nil {
			continue
		}
		if current, exists := r.providersByID[config.Providers[index].ID]; exists {
			if current.Credential != nil {
				credentialCopy := *current.Credential
				config.Providers[index].Credential = &credentialCopy
			}
		}
	}
	providerClients, err := buildProviderClients(config.Providers, r.client)
	if err != nil {
		return err
	}
	if err := writeConfigAtomically(r.modelsPath, config); err != nil {
		return &ConfigurationError{Path: r.modelsPath, Err: err}
	}
	modelsByKey, providersByID := buildIndexes(config.Providers, validated.models)
	r.config = config
	r.modelsByKey = modelsByKey
	r.providersByID = providersByID
	r.providerClients = providerClients
	return nil
}

// SetProviderKey stores or clears the API key associated with one provider.
func (r *Registry) SetProviderKey(providerID, value string) error {
	if r == nil {
		return errors.New("registry is not initialized")
	}
	providerID = strings.TrimSpace(providerID)
	if !idPattern.MatchString(providerID) {
		return fmt.Errorf("invalid provider id %q", providerID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providersByID[providerID]; !exists {
		return fmt.Errorf("provider %q is not configured", providerID)
	}
	config := r.configLocked(true)
	value = strings.TrimSpace(value)
	for index := range config.Providers {
		if config.Providers[index].ID == providerID {
			if value == "" {
				config.Providers[index].Credential = nil
			} else {
				config.Providers[index].Credential = &CredentialRecord{
					Type: CredentialTypeAPIKey,
					Key:  value,
				}
			}
			if err := writeConfigAtomically(r.modelsPath, config); err != nil {
				return &ConfigurationError{Path: r.modelsPath, Err: err}
			}
			r.config = config
			r.providersByID[providerID] = config.Providers[index]
			return nil
		}
	}
	return fmt.Errorf("provider %q is not present in model configuration", providerID)
}

// ProviderKey returns the API key stored for a configured provider.
func (r *Registry) ProviderKey(providerID string) string {
	if r == nil || !idPattern.MatchString(providerID) {
		return ""
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, exists := r.providersByID[providerID]; !exists {
		return ""
	}
	return providerKey(r.providersByID[providerID])
}

// HasProviderKey reports secret presence without exposing the secret value to
// application or binding layers.
func (r *Registry) HasProviderKey(providerID string) bool {
	return r.ProviderKey(providerID) != ""
}
