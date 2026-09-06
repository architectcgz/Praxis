package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"praxis/internal/providers"
)

// profileNames is the bindable agent profile allowlist shared by the config
// file and the settings UI.
var profileNames = []string{"primary", "delegate", "advisor", "curator"}

var defaultReasoningLevels = []string{"low", "medium", "high"}

const defaultReasoningLevel = "medium"

// ProfileNames returns the bindable profile allowlist so the settings UI can
// render exactly the bindings that will pass validation.
func ProfileNames() []string {
	return append([]string(nil), profileNames...)
}

// Validate checks a whole model configuration and returns the normalized result
// with defaults applied and reasoning levels deduplicated.
// Loading from disk and saving from the UI share these rules so the two paths
// cannot drift: an error surfaced before saving is the same verdict the next
// startup would reach. The argument is never mutated; the result is a deep copy.
func Validate(config FileConfig) (FileConfig, error) {
	config.Groups = append([]GroupConfig(nil), config.Groups...)
	config.Providers = append([]ProviderConfig(nil), config.Providers...)
	for index := range config.Providers {
		config.Providers[index].Models = cloneModels(config.Providers[index].Models)
		if credential := config.Providers[index].Credential; credential != nil {
			credentialCopy := *credential
			config.Providers[index].Credential = &credentialCopy
		}
	}
	profiles := make(map[string]ModelReference, len(config.Profiles))
	for key, value := range config.Profiles {
		profiles[key] = value
	}
	config.Profiles = profiles
	groupsSeen, err := validateGroups(config.Groups)
	if err != nil {
		return FileConfig{}, err
	}
	modelsSeen, err := validateProviders(config.Providers, groupsSeen)
	if err != nil {
		return FileConfig{}, err
	}
	for profile, reference := range config.Profiles {
		reference.ProviderID = strings.TrimSpace(reference.ProviderID)
		reference.ModelID = strings.TrimSpace(reference.ModelID)
		config.Profiles[profile] = reference
		if !isKnownProfile(profile) {
			return FileConfig{}, fmt.Errorf("models config: unknown profile %q", profile)
		}
		if _, exists := modelsSeen[modelKey{strings.TrimSpace(reference.ProviderID), strings.TrimSpace(reference.ModelID)}]; !exists {
			return FileConfig{}, fmt.Errorf(
				"models config: profiles.%s references unknown model %q for provider %q", profile, reference.ModelID, reference.ProviderID,
			)
		}
	}
	return config, nil
}

func isKnownProfile(profile string) bool {
	for _, known := range profileNames {
		if profile == known {
			return true
		}
	}
	return false
}

func validateGroups(configs []GroupConfig) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(configs))
	for index := range configs {
		group := &configs[index]
		group.ID = strings.TrimSpace(group.ID)
		group.DisplayName = strings.TrimSpace(group.DisplayName)
		if !idPattern.MatchString(group.ID) {
			return nil, fmt.Errorf("models config: invalid group id %q", group.ID)
		}
		if group.DisplayName == "" {
			return nil, fmt.Errorf("models config: group %q display name is required", group.ID)
		}
		if _, exists := seen[group.ID]; exists {
			return nil, fmt.Errorf("models config: duplicate group id %q", group.ID)
		}
		seen[group.ID] = struct{}{}
	}
	return seen, nil
}

func validateProviders(configs []ProviderConfig, groupsSeen map[string]struct{}) (map[modelKey]struct{}, error) {
	seen := make(map[string]struct{}, len(configs))
	modelsSeen := make(map[modelKey]struct{})
	for index := range configs {
		provider := &configs[index]
		provider.ID = strings.TrimSpace(provider.ID)
		provider.DisplayName = strings.TrimSpace(provider.DisplayName)
		provider.BaseURL = strings.TrimSpace(provider.BaseURL)
		provider.ProxyURL = strings.TrimSpace(provider.ProxyURL)
		if !idPattern.MatchString(provider.ID) {
			return nil, fmt.Errorf("models config: invalid provider id %q", provider.ID)
		}
		if _, exists := seen[provider.ID]; exists {
			return nil, fmt.Errorf("models config: duplicate provider id %q", provider.ID)
		}
		seen[provider.ID] = struct{}{}
		if _, err := providers.ValidateBaseURL(provider.BaseURL); err != nil {
			return nil, fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		if _, err := providers.ValidateProxyURL(provider.ProxyURL); err != nil {
			return nil, fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		if provider.DisplayName == "" {
			provider.DisplayName = provider.ID
		}
		if !validAPIFormat(provider.DefaultAPIFormat) {
			return nil, fmt.Errorf("models config: provider %q has unsupported default API format %q", provider.ID, provider.DefaultAPIFormat)
		}
		if err := validateCredential(provider); err != nil {
			return nil, fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		providerModels, err := validateProviderModels(provider, groupsSeen, modelsSeen)
		if err != nil {
			return nil, err
		}
		for key := range providerModels {
			modelsSeen[key] = struct{}{}
		}
	}
	return modelsSeen, nil
}

func validateCredential(provider *ProviderConfig) error {
	if provider.Credential == nil {
		return nil
	}
	provider.Credential.Type = CredentialType(strings.TrimSpace(string(provider.Credential.Type)))
	provider.Credential.Key = strings.TrimSpace(provider.Credential.Key)
	if provider.Credential.Type != CredentialTypeAPIKey {
		return fmt.Errorf("unsupported credential type %q", provider.Credential.Type)
	}
	if provider.Credential.Key == "" {
		return errors.New("credential key is required")
	}
	return nil
}

func validateProviderModels(provider *ProviderConfig, groupsSeen map[string]struct{}, existing map[modelKey]struct{}) (map[modelKey]struct{}, error) {
	seen := make(map[modelKey]struct{}, len(provider.Models))
	for index := range provider.Models {
		model := &provider.Models[index]
		model.ID = strings.TrimSpace(model.ID)
		model.DisplayName = strings.TrimSpace(model.DisplayName)
		model.GroupID = strings.TrimSpace(model.GroupID)
		if model.ID == "" {
			return nil, fmt.Errorf("models config: provider %q model id is required", provider.ID)
		}
		if model.DisplayName == "" {
			model.DisplayName = model.ID
		}
		if _, exists := groupsSeen[model.GroupID]; !exists {
			return nil, fmt.Errorf("models config: model %q for provider %q references unknown group %q", model.ID, provider.ID, model.GroupID)
		}
		key := modelKey{provider.ID, model.ID}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("models config: duplicate model %q for provider %q", model.ID, provider.ID)
		}
		if _, exists := existing[key]; exists {
			return nil, fmt.Errorf("models config: duplicate model %q for provider %q", model.ID, provider.ID)
		}
		seen[key] = struct{}{}
		if model.APIFormatOverride != nil && !validAPIFormat(*model.APIFormatOverride) {
			return nil, fmt.Errorf("models config: model %q has unsupported API format %q", model.ID, *model.APIFormatOverride)
		}
		if model.ContextWindow <= 0 || model.MaxOutputTokens <= 0 || model.MaxOutputTokens >= model.ContextWindow {
			return nil, fmt.Errorf("models config: model %q has invalid capability limits", model.ID)
		}
		normalized, err := normalizeReasoning(model.Reasoning)
		if err != nil {
			return nil, fmt.Errorf("models config: model %q: %w", model.ID, err)
		}
		model.Reasoning = normalized
	}
	return seen, nil
}

func validAPIFormat(format ModelAPIFormat) bool {
	switch format {
	case APIFormatAnthropicMessages, APIFormatOpenAIResponses, APIFormatOpenAIChatCompletions:
		return true
	default:
		return false
	}
}

// writeJSONAtomic writes a sibling temporary file and renames it over the
// target, so a failure mid-write cannot leave truncated JSON that blocks the
// next startup. Mode stays 0600: both files may hold sensitive material.
func writeJSONAtomic(path string, payload any) error {
	encoded, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("encode %s: %w", filepath.Base(path), err)
	}
	encoded = append(encoded, '\n')
	directory := filepath.Dir(path)
	temporary, err := os.CreateTemp(directory, filepath.Base(path)+".*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary config: %w", err)
	}
	temporaryPath := temporary.Name()
	cleanup := func() { _ = os.Remove(temporaryPath) }
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("secure temporary config: %w", err)
	}
	if _, err := temporary.Write(encoded); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("write temporary config: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		cleanup()
		return fmt.Errorf("flush temporary config: %w", err)
	}
	if err := temporary.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temporary config: %w", err)
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		cleanup()
		return fmt.Errorf("replace config: %w", err)
	}
	return nil
}

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
	validated.Revision = r.config.Revision + 1
	providerClients, err := buildProviderClients(validated.Providers, r.client)
	if err != nil {
		return err
	}
	if err := writeJSONAtomic(r.modelsPath, validated); err != nil {
		return &ConfigurationError{Path: r.modelsPath, Err: err}
	}
	byModel := make(map[modelKey]ModelConfig)
	byProvider := make(map[string]ProviderConfig, len(validated.Providers))
	for _, provider := range validated.Providers {
		byProvider[provider.ID] = provider
		for _, model := range provider.Models {
			byModel[modelKey{provider.ID, model.ID}] = model
		}
	}
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
			if err := writeJSONAtomic(r.modelsPath, config); err != nil {
				return &ConfigurationError{Path: r.modelsPath, Err: err}
			}
			config.Revision = r.config.Revision + 1
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
