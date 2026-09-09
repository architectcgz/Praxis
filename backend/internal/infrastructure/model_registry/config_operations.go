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
func (r *Registry) ApplyConfig(config FileConfig) error {
	if r == nil {
		return errors.New("registry is not initialized")
	}
	validated, err := Validate(config)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for index := range validated.Providers {
		if validated.Providers[index].Credential != nil {
			continue
		}
		if current, exists := r.byProvider[validated.Providers[index].ID]; exists {
			if current.Credential != nil {
				credentialCopy := *current.Credential
				validated.Providers[index].Credential = &credentialCopy
			}
		}
	}
	providerClients, err := buildProviderClients(validated.Providers, r.client)
	if err != nil {
		return err
	}
	if err := writeConfigAtomically(r.modelsPath, validated); err != nil {
		return &ConfigurationError{Path: r.modelsPath, Err: err}
	}
	byModel, byProvider := buildIndexes(validated.Providers)
	r.config = validated
	r.byModel = byModel
	r.byProvider = byProvider
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
	if _, exists := r.byProvider[providerID]; !exists {
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
			r.byProvider[providerID] = config.Providers[index]
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
	if _, exists := r.byProvider[providerID]; !exists {
		return ""
	}
	return providerKey(r.byProvider[providerID])
}

// HasProviderKey reports secret presence without exposing the secret value to
// application or binding layers.
func (r *Registry) HasProviderKey(providerID string) bool {
	return r.ProviderKey(providerID) != ""
}
