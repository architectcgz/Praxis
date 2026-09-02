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
	config.Providers = append([]ProviderConfig(nil), config.Providers...)
	config.Models = cloneModels(config.Models)
	profiles := make(map[string]ModelReference, len(config.Profiles))
	for key, value := range config.Profiles {
		profiles[key] = value
	}
	config.Profiles = profiles
	providersSeen, err := validateProviders(config.Providers)
	if err != nil {
		return FileConfig{}, err
	}
	modelsSeen, err := validateModels(config.Models, providersSeen)
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

func validateProviders(configs []ProviderConfig) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(configs))
	for _, provider := range configs {
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
	}
	return seen, nil
}

func validateModels(configs []ModelConfig, providersSeen map[string]struct{}) (map[modelKey]struct{}, error) {
	seen := make(map[modelKey]struct{}, len(configs))
	for index := range configs {
		model := &configs[index]
		model.ProviderID = strings.TrimSpace(model.ProviderID)
		model.ModelID = strings.TrimSpace(model.ModelID)
		if model.ProviderID == "" {
			return nil, errors.New("models config: model provider id is required")
		}
		if model.ModelID == "" {
			return nil, errors.New("models config: model id is required")
		}
		key := modelKey{model.ProviderID, model.ModelID}
		if _, exists := seen[key]; exists {
			return nil, fmt.Errorf("models config: duplicate model %q for provider %q", model.ModelID, model.ProviderID)
		}
		seen[key] = struct{}{}
		if _, exists := providersSeen[model.ProviderID]; !exists {
			return nil, fmt.Errorf(
				"models config: model %q references unknown provider %q", model.ModelID, model.ProviderID,
			)
		}
		if !validAPIFormat(model.APIFormat) {
			return nil, fmt.Errorf(
				"models config: model %q has unsupported API format %q", model.ModelID, model.APIFormat,
			)
		}
		if model.ContextWindow <= 0 ||
			model.MaxOutputTokens <= 0 || model.MaxOutputTokens >= model.ContextWindow {
			return nil, fmt.Errorf("models config: model %q has invalid capability limits", model.ModelID)
		}
		normalized, err := normalizeReasoning(model.Reasoning)
		if err != nil {
			return nil, fmt.Errorf("models config: model %q: %w", model.ModelID, err)
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
	providerClients, err := buildProviderClients(validated.Providers, r.client)
	if err != nil {
		return err
	}
	if err := writeJSONAtomic(r.modelsPath, validated); err != nil {
		return &ConfigurationError{Path: r.modelsPath, Err: err}
	}
	byModel := make(map[modelKey]ModelConfig, len(validated.Models))
	for _, model := range validated.Models {
		byModel[modelKey{model.ProviderID, model.ModelID}] = model
	}
	byProvider := make(map[string]ProviderConfig, len(validated.Providers))
	for _, provider := range validated.Providers {
		byProvider[provider.ID] = provider
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	keys := make(map[string]string, len(byProvider))
	for providerID := range byProvider {
		if key := strings.TrimSpace(r.secrets[providerID]); key != "" {
			keys[providerID] = key
		}
	}
	if err := writeJSONAtomic(r.secretsPath, secretsFile{Keys: keys}); err != nil {
		return &ConfigurationError{Path: r.secretsPath, Err: err}
	}
	r.config = validated
	r.byModel = byModel
	r.byProvider = byProvider
	r.providerClients = providerClients
	r.secrets = keys
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
	keys := make(map[string]string, len(r.secrets)+1)
	for key, secret := range r.secrets {
		keys[key] = secret
	}
	if value = strings.TrimSpace(value); value == "" {
		delete(keys, providerID)
	} else {
		keys[providerID] = value
	}
	if err := writeJSONAtomic(r.secretsPath, secretsFile{Keys: keys}); err != nil {
		return &ConfigurationError{Path: r.secretsPath, Err: err}
	}
	r.secrets = keys
	return nil
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
	return strings.TrimSpace(r.secrets[providerID])
}

// HasProviderKey reports secret presence without exposing the secret value to
// application or binding layers.
func (r *Registry) HasProviderKey(providerID string) bool {
	return r.ProviderKey(providerID) != ""
}
