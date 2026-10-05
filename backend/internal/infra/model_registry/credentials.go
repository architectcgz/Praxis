package modelregistry

import (
	"errors"
	"fmt"
	"strings"
)

// normalizeCredentials validates the persisted auth document and returns a
// deep copy with normalized provider IDs, credential types, and keys.
//
// Entries for providers that are not part of the current model configuration
// are preserved: the two documents have separate lifecycles, and a credential
// only becomes effective when a configured provider resolves it by ID.
func normalizeCredentials(document ProviderCredentials) (ProviderCredentials, error) {
	normalized := make(ProviderCredentials, len(document))
	for providerID, credential := range document {
		id := strings.TrimSpace(providerID)
		if !idPattern.MatchString(id) {
			return nil, fmt.Errorf("auth config: invalid provider id %q", id)
		}
		credential.Type = CredentialType(strings.TrimSpace(string(credential.Type)))
		credential.Key = strings.TrimSpace(credential.Key)
		if credential.Type != CredentialTypeAPIKey {
			return nil, fmt.Errorf("auth config: provider %q: unsupported credential type %q", id, credential.Type)
		}
		if credential.Key == "" {
			return nil, fmt.Errorf("auth config: provider %q: credential key is required", id)
		}
		normalized[id] = credential
	}
	return normalized, nil
}

// SetProviderKey stores or clears the API key associated with one configured
// provider. Only the auth document is written; the model configuration
// document never contains a secret.
func (r *Registry) SetProviderKey(providerID, value string) error {
	if r == nil {
		return errors.New("registry is not initialized")
	}
	if !idPattern.MatchString(providerID) {
		return fmt.Errorf("invalid provider id %q", providerID)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.providersByID[providerID]; !exists {
		return fmt.Errorf("provider %q is not configured", providerID)
	}
	credentials := cloneCredentials(r.credentials)
	value = strings.TrimSpace(value)
	if value == "" {
		delete(credentials, providerID)
	} else {
		credentials[providerID] = ProviderCredential{Type: CredentialTypeAPIKey, Key: value}
	}
	if !credentialsChanged(r.credentials, credentials) {
		return nil
	}
	if r.credentialsPath != "" {
		if err := writeConfigAtomically(r.credentialsPath, credentials); err != nil {
			return &ConfigurationError{Path: r.credentialsPath, Err: err}
		}
	}
	r.credentials = credentials
	return nil
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
	return r.credentialKeyLocked(providerID)
}

// HasProviderKey reports secret presence without exposing the secret value to
// application or binding layers.
func (r *Registry) HasProviderKey(providerID string) bool {
	return r.ProviderKey(providerID) != ""
}

// credentialKeyLocked resolves the active API key for one provider. The caller
// must hold r.mu; runtime resolution is the only path allowed to read the key.
func (r *Registry) credentialKeyLocked(providerID string) string {
	credential, exists := r.credentials[providerID]
	if !exists || credential.Type != CredentialTypeAPIKey {
		return ""
	}
	return credential.Key
}

func cloneCredentials(credentials ProviderCredentials) ProviderCredentials {
	cloned := make(ProviderCredentials, len(credentials)+1)
	for providerID, credential := range credentials {
		cloned[providerID] = credential
	}
	return cloned
}

// retainCredentialsForProviders returns the credentials that belong to the
// configured providers, so persisting the next model configuration also drops
// the secret of any removed provider. Providers without a stored credential
// stay absent instead of gaining an empty entry.
func retainCredentialsForProviders(credentials ProviderCredentials, providers []ProviderConfig) ProviderCredentials {
	retained := make(ProviderCredentials, len(providers))
	for _, provider := range providers {
		if credential, exists := credentials[provider.ID]; exists {
			retained[provider.ID] = credential
		}
	}
	return retained
}

func credentialsChanged(current, next ProviderCredentials) bool {
	if len(current) != len(next) {
		return true
	}
	for providerID, credential := range next {
		existing, exists := current[providerID]
		if !exists || existing != credential {
			return true
		}
	}
	return false
}
