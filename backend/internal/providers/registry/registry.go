// Package registry loads the user-owned model and secret configuration and
// materializes provider-neutral model interfaces.
package registry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"

	domainsecurity "praxis/internal/domain/security"

	providerapi "praxis/internal/providers"
	"praxis/internal/providers/anthropic"
	"praxis/internal/providers/openaicompat"
	runtimecontract "praxis/internal/runtime"
)

const maxProviderCatalogBytes = 1 << 20

type ProviderConfig struct {
	ID               string            `json:"id"`
	DisplayName      string            `json:"displayName"`
	BaseURL          string            `json:"baseUrl"`
	ProxyURL         string            `json:"proxyUrl,omitempty"`
	Credential       *CredentialRecord `json:"credential,omitempty"`
	DefaultAPIFormat ModelAPIFormat    `json:"defaultApiFormat"`
	Models           []ModelConfig     `json:"models"`
}

type CredentialRecord struct {
	Type CredentialType `json:"type"`
	Key  string         `json:"key"`
}

type CredentialType string

const CredentialTypeAPIKey CredentialType = "api_key"

type GroupConfig struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

type ModelAPIFormat string

const (
	APIFormatAnthropicMessages     ModelAPIFormat = "anthropic_messages"
	APIFormatOpenAIResponses       ModelAPIFormat = "openai_responses"
	APIFormatOpenAIChatCompletions ModelAPIFormat = "openai_chat_completions"
)

type ModelConfig struct {
	ID                string          `json:"id"`
	DisplayName       string          `json:"displayName"`
	GroupID           string          `json:"groupId"`
	APIFormatOverride *ModelAPIFormat `json:"apiFormatOverride,omitempty"`
	ContextWindow     int             `json:"contextWindow"`
	MaxOutputTokens   int             `json:"maxOutputTokens"`
	Reasoning         ReasoningConfig `json:"reasoning,omitempty"`
}

// ReasoningConfig declares the levels a confirmed model accepts. The provider
// adapter maps an approved level to its protocol-specific request field.
type ReasoningConfig struct {
	Supported bool     `json:"supported"`
	Levels    []string `json:"levels,omitempty"`
	Default   string   `json:"default,omitempty"`
}

// ModelOption is the execution catalog exposed to desktop clients.
type ModelOption struct {
	GroupID         string
	ProviderID      string
	ModelID         string
	Label           string
	ProviderName    string
	Reasoning       ReasoningConfig
	DefaultProfiles []string
}

type FileConfig struct {
	Revision  uint64                    `json:"revision"`
	Groups    []GroupConfig             `json:"groups"`
	Providers []ProviderConfig          `json:"providers"`
	Profiles  map[string]ModelReference `json:"profiles"`
}

type ModelReference struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

type modelKey struct {
	ProviderID string
	ModelID    string
}

// Registry is the single in-process owner of the model configuration. Saving
// from the UI replaces the config and its indexes in place, so every read path
// must hold mu; holders of this pointer never observe the swap.
type Registry struct {
	mu              sync.RWMutex
	modelsPath      string
	config          FileConfig
	client          *http.Client
	providerClients map[string]*http.Client
	byModel         map[modelKey]ModelConfig
	byProvider      map[string]ProviderConfig
}

// ConfigurationError identifies the configuration file that prevented the
// registry from loading. Its cause never contains a resolved secret value.
type ConfigurationError struct {
	Path string
	Err  error
}

func (e *ConfigurationError) Error() string {
	if e == nil {
		return "configuration error"
	}
	return fmt.Sprintf("configuration %q: %v", e.Path, e.Err)
}

func (e *ConfigurationError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

func Load(modelsPath string, client *http.Client) (*Registry, error) {
	config, err := loadModels(modelsPath)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	providerClients, err := buildProviderClients(config.Providers, client)
	if err != nil {
		return nil, &ConfigurationError{Path: modelsPath, Err: err}
	}
	registry := &Registry{
		modelsPath: modelsPath,
		config:     config, client: client, providerClients: providerClients,
		byModel: make(map[modelKey]ModelConfig), byProvider: make(map[string]ProviderConfig),
	}
	for _, provider := range config.Providers {
		registry.byProvider[provider.ID] = provider
		for _, model := range provider.Models {
			registry.byModel[modelKey{provider.ID, model.ID}] = model
		}
	}
	return registry, nil
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

func (r *Registry) providerClientLocked(providerID string) *http.Client {
	if client := r.providerClients[providerID]; client != nil {
		return client
	}
	return providerapi.RequestClient(r.client)
}

// Config returns a deep copy of the current configuration for the settings UI.
func (r *Registry) Config() FileConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.configLocked(false)
}

func (r *Registry) configLocked(includeCredentials bool) FileConfig {
	copy := r.config
	copy.Groups = append([]GroupConfig(nil), r.config.Groups...)
	copy.Providers = append([]ProviderConfig(nil), r.config.Providers...)
	for index := range copy.Providers {
		copy.Providers[index].Models = cloneModels(r.config.Providers[index].Models)
		if includeCredentials {
			if credential := r.config.Providers[index].Credential; credential != nil {
				credentialCopy := *credential
				copy.Providers[index].Credential = &credentialCopy
			}
		} else {
			copy.Providers[index].Credential = nil
		}
	}
	copy.Profiles = make(map[string]ModelReference, len(r.config.Profiles))
	for key, value := range r.config.Profiles {
		copy.Profiles[key] = value
	}
	return copy
}

func cloneModels(models []ModelConfig) []ModelConfig {
	copy := append([]ModelConfig(nil), models...)
	for index := range copy {
		copy[index].Reasoning.Levels = append([]string(nil), models[index].Reasoning.Levels...)
	}
	return copy
}

func (r *Registry) Model(providerID, modelID string) (ModelConfig, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.modelLocked(providerID, modelID)
}

func (r *Registry) modelLocked(providerID, modelID string) (ModelConfig, error) {
	providerID = strings.TrimSpace(providerID)
	modelID = strings.TrimSpace(modelID)
	model, ok := r.byModel[modelKey{providerID, modelID}]
	if !ok {
		return ModelConfig{}, fmt.Errorf("model %q for provider %q is not configured", modelID, providerID)
	}
	return model, nil
}

// ListModels returns confirmed models that may be selected for the next
// execution. The profile list identifies configured defaults only.
func (r *Registry) ListModels() []ModelOption {
	r.mu.RLock()
	defer r.mu.RUnlock()
	options := make([]ModelOption, 0)
	for _, provider := range r.config.Providers {
		for _, model := range provider.Models {
			profiles := make([]string, 0, len(r.config.Profiles))
			for profile, modelID := range r.config.Profiles {
				if modelID.ProviderID == provider.ID && modelID.ModelID == model.ID {
					profiles = append(profiles, profile)
				}
			}
			sort.Strings(profiles)
			label := strings.TrimSpace(model.DisplayName)
			if label == "" {
				label = model.ID
			}
			providerName := strings.TrimSpace(provider.DisplayName)
			if providerName == "" {
				providerName = provider.BaseURL
			}
			options = append(options, ModelOption{
				GroupID: model.GroupID, ProviderID: provider.ID, ModelID: model.ID, Label: label, ProviderName: providerName,
				Reasoning: cloneReasoning(model.Reasoning), DefaultProfiles: profiles,
			})
		}
	}
	return orderModelOptions(options, r.config.Groups)
}

func orderModelOptions(options []ModelOption, groups []GroupConfig) []ModelOption {
	groupOrder := make(map[string]int, len(groups))
	for index, group := range groups {
		groupOrder[group.ID] = index
	}
	sort.SliceStable(options, func(i, j int) bool {
		leftGroup, leftOK := groupOrder[options[i].GroupID]
		rightGroup, rightOK := groupOrder[options[j].GroupID]
		if leftOK && rightOK && leftGroup != rightGroup {
			return leftGroup < rightGroup
		}
		if leftOK != rightOK {
			return leftOK
		}
		if options[i].Label == options[j].Label {
			if options[i].ProviderID == options[j].ProviderID {
				return options[i].ModelID < options[j].ModelID
			}
			return options[i].ProviderID < options[j].ProviderID
		}
		return options[i].Label < options[j].Label
	})
	return options
}

// DiscoverProviderModels retrieves the model IDs advertised by one configured
// OpenAI-compatible provider. Credentials are resolved in-process and never
// returned to the caller.
func (r *Registry) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
	if r == nil {
		return nil, errors.New("model registry is not initialized")
	}
	if ctx == nil {
		return nil, errors.New("provider model discovery context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	providerID = strings.TrimSpace(providerID)
	r.mu.RLock()
	provider, exists := r.byProvider[providerID]
	client := r.providerClientLocked(providerID)
	r.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("provider %q is not configured", providerID)
	}
	baseURL, err := providerapi.ValidateBaseURL(provider.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", providerID, err)
	}
	key := r.ProviderKey(providerID)
	if key == "" {
		return nil, fmt.Errorf("API key is not configured for provider %q", providerID)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/v1/models", nil)
	if err != nil {
		return nil, fmt.Errorf("build provider model request: %w", err)
	}
	request.Header.Set("Authorization", "Bearer "+key)
	request.Header.Set("Accept", "application/json")
	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("retrieve provider models: %w", err)
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return nil, providerapi.DecodeErrorResponse(response)
	}
	defer response.Body.Close()
	var payload struct {
		Data json.RawMessage `json:"data"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, maxProviderCatalogBytes))
	if err := decoder.Decode(&payload); err != nil {
		return nil, errors.New("provider model catalog response is invalid")
	}
	if err := ensureEOF(decoder); err != nil {
		return nil, errors.New("provider model catalog response is invalid")
	}
	if len(payload.Data) == 0 || string(bytes.TrimSpace(payload.Data)) == "null" {
		return nil, errors.New("provider model catalog response is invalid")
	}
	var items []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(payload.Data, &items); err != nil {
		return nil, errors.New("provider model catalog response is invalid")
	}
	seen := make(map[string]struct{}, len(items))
	models := make([]string, 0, len(items))
	for _, item := range items {
		id := strings.TrimSpace(item.ID)
		if id == "" {
			continue
		}
		if _, exists := seen[id]; exists {
			continue
		}
		seen[id] = struct{}{}
		models = append(models, id)
	}
	sort.Strings(models)
	return models, nil
}

// ResolveModel returns the configured provider/model reference for an agent profile. Profiles
// are resolved when a Grant is created so durable executions retain the exact
// model selection that was approved for them.
func (r *Registry) ResolveModel(profile domainsecurity.AgentProfile) (domainsecurity.ModelSelection, error) {
	if !profile.Valid() {
		return domainsecurity.ModelSelection{}, fmt.Errorf("unknown agent profile %q", profile)
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	reference, ok := r.config.Profiles[string(profile)]
	if !ok || strings.TrimSpace(reference.ProviderID) == "" || strings.TrimSpace(reference.ModelID) == "" {
		return domainsecurity.ModelSelection{}, fmt.Errorf("model profile %q is not configured", profile)
	}
	model, err := r.modelLocked(reference.ProviderID, reference.ModelID)
	if err != nil {
		return domainsecurity.ModelSelection{}, fmt.Errorf("model profile %q: %w", profile, err)
	}
	return r.modelSelection(reference.ProviderID, model, "")
}

// ResolveModelSelection validates a model and optional thinking level before
// the orchestration layer freezes it into an execution-specific Grant.
func (r *Registry) ResolveModelSelection(providerID, modelID, reasoning string) (domainsecurity.ModelSelection, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	model, err := r.modelLocked(providerID, modelID)
	if err != nil {
		return domainsecurity.ModelSelection{}, err
	}
	return r.modelSelection(providerID, model, reasoning)
}

// Stream resolves a configured provider/model selection into a model stream.
func (r *Registry) Stream(providerID, modelID string) (runtimecontract.ModelStream, error) {
	return r.StreamFor(domainsecurity.ModelSelection{ProviderID: providerID, ModelID: modelID})
}

// StreamFor builds a stream for the already-frozen model selection.
func (r *Registry) StreamFor(selection domainsecurity.ModelSelection) (runtimecontract.ModelStream, error) {
	if strings.TrimSpace(selection.ProviderID) == "" || strings.TrimSpace(selection.ModelID) == "" {
		return nil, errors.New("model selection provider and model IDs are required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	model, err := r.modelLocked(selection.ProviderID, selection.ModelID)
	if err != nil {
		return nil, err
	}
	selection, err = r.modelSelection(selection.ProviderID, model, selection.Reasoning)
	if err != nil {
		return nil, err
	}
	provider, ok := r.byProvider[selection.ProviderID]
	if !ok {
		return nil, fmt.Errorf("provider %q is not configured", selection.ProviderID)
	}
	client := r.providerClientLocked(provider.ID)
	key := providerKey(provider)
	if key == "" {
		return nil, fmt.Errorf("API key is not configured for provider %q", provider.ID)
	}
	apiFormat := EffectiveAPIFormat(provider, model)
	switch apiFormat {
	case APIFormatAnthropicMessages:
		return anthropic.New(anthropic.Config{
			BaseURL: provider.BaseURL, APIKey: key, HTTPClient: client,
			Model: model.ID, MaxOutputTokens: model.MaxOutputTokens,
			Reasoning: selection.Reasoning,
		})
	case APIFormatOpenAIResponses, APIFormatOpenAIChatCompletions:
		protocol := openaicompat.ProtocolResponses
		if apiFormat == APIFormatOpenAIChatCompletions {
			protocol = openaicompat.ProtocolChatCompletions
		}
		return openaicompat.New(openaicompat.Config{
			BaseURL: provider.BaseURL, APIKey: key, HTTPClient: client,
			Protocol: protocol,
			Model:    model.ID, MaxOutputTokens: model.MaxOutputTokens,
			Reasoning: selection.Reasoning,
		})
	default:
		return nil, fmt.Errorf("model %q has unsupported API format %q", model.ID, apiFormat)
	}
}

func (r *Registry) modelSelection(providerID string, model ModelConfig, reasoning string) (domainsecurity.ModelSelection, error) {
	reasoning = strings.TrimSpace(reasoning)
	config := model.Reasoning
	if !config.Supported {
		if reasoning != "" {
			return domainsecurity.ModelSelection{}, fmt.Errorf("model %q does not support reasoning", model.ID)
		}
		return domainsecurity.ModelSelection{ProviderID: providerID, ModelID: model.ID}, nil
	}
	if reasoning == "" {
		reasoning = config.Default
	}
	if !containsReasoningLevel(config.Levels, reasoning) {
		return domainsecurity.ModelSelection{}, fmt.Errorf("model %q does not support reasoning %q", model.ID, reasoning)
	}
	return domainsecurity.ModelSelection{ProviderID: providerID, ModelID: model.ID, Reasoning: reasoning}, nil
}

func cloneReasoning(config ReasoningConfig) ReasoningConfig {
	return ReasoningConfig{
		Supported: config.Supported,
		Levels:    append([]string(nil), config.Levels...),
		Default:   config.Default,
	}
}

func containsReasoningLevel(levels []string, target string) bool {
	for _, level := range levels {
		if level == target {
			return true
		}
	}
	return false
}

func normalizeReasoning(config ReasoningConfig) (ReasoningConfig, error) {
	config.Default = strings.TrimSpace(config.Default)
	if !config.Supported {
		if len(config.Levels) > 0 || config.Default != "" {
			return ReasoningConfig{}, errors.New("unsupported reasoning cannot declare levels or a default")
		}
		return defaultReasoningConfig(), nil
	}
	if len(config.Levels) == 0 {
		return ReasoningConfig{}, errors.New("supported reasoning requires levels")
	}
	levels := make([]string, 0, len(config.Levels))
	seen := make(map[string]struct{}, len(config.Levels))
	for _, level := range config.Levels {
		level = strings.TrimSpace(level)
		if level == "" {
			return ReasoningConfig{}, errors.New("reasoning levels cannot be empty")
		}
		if _, exists := seen[level]; exists {
			return ReasoningConfig{}, fmt.Errorf("duplicate reasoning level %q", level)
		}
		seen[level] = struct{}{}
		levels = append(levels, level)
	}
	if config.Default == "" {
		config.Default = defaultReasoningLevel
	}
	if !containsReasoningLevel(levels, config.Default) {
		return ReasoningConfig{}, fmt.Errorf("reasoning default %q is not supported", config.Default)
	}
	config.Levels = levels
	return config, nil
}

func defaultReasoningConfig() ReasoningConfig {
	return ReasoningConfig{
		Supported: true,
		Levels:    append([]string(nil), defaultReasoningLevels...),
		Default:   defaultReasoningLevel,
	}
}

func loadModels(path string) (FileConfig, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		config := FileConfig{
			Groups: []GroupConfig{}, Providers: []ProviderConfig{},
			Profiles: map[string]ModelReference{},
		}
		payload, marshalErr := json.MarshalIndent(config, "", "  ")
		if marshalErr != nil {
			return FileConfig{}, fmt.Errorf("encode models config template: %w", marshalErr)
		}
		if writeErr := os.WriteFile(path, append(payload, '\n'), 0o600); writeErr != nil {
			return FileConfig{}, fmt.Errorf("create models config: %w", writeErr)
		}
		file, err = os.Open(path)
	}
	if err != nil {
		return FileConfig{}, fmt.Errorf("open models config: %w", err)
	}
	defer file.Close()
	var config FileConfig
	decoder := json.NewDecoder(file)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&config); err != nil {
		return FileConfig{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if err := ensureEOF(decoder); err != nil {
		return FileConfig{}, fmt.Errorf("models config: invalid JSON: %w", err)
	}
	if config.Profiles == nil {
		config.Profiles = map[string]ModelReference{}
	}
	return Validate(config)
}

func ensureEOF(decoder *json.Decoder) error {
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return errors.New("trailing data")
	}
	return nil
}

func providerKey(provider ProviderConfig) string {
	if provider.Credential == nil || provider.Credential.Type != CredentialTypeAPIKey {
		return ""
	}
	return strings.TrimSpace(provider.Credential.Key)
}

// EffectiveAPIFormat returns the protocol selected by the provider default and
// an optional model override.
func EffectiveAPIFormat(provider ProviderConfig, model ModelConfig) ModelAPIFormat {
	if model.APIFormatOverride != nil {
		return *model.APIFormatOverride
	}
	return provider.DefaultAPIFormat
}
