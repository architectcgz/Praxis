// Package modelregistry loads the user-owned model and secret configuration and
// materializes provider-neutral model interfaces.
package modelregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"

	domainmodel "praxis/internal/domain/model"

	providerapi "praxis/internal/providers"
	"praxis/internal/providers/anthropic"
	"praxis/internal/providers/openaicompat"
	runtimecontract "praxis/internal/runtime"
)

const maxProviderCatalogBytes = 1 << 20

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

// ListModels returns confirmed models and their configured reasoning levels for
// the next execution.
func (r *Registry) ListModels() []ModelOption {
	r.mu.RLock()
	defer r.mu.RUnlock()
	options := make([]ModelOption, 0)
	for _, provider := range r.config.Providers {
		for _, model := range provider.Models {
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
				ReasoningLevels:       append([]string(nil), model.ReasoningLevels...),
				DefaultReasoningLevel: model.DefaultReasoningLevel,
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

// ResolveModelSelection validates a model and optional thinking level before
// the orchestration layer freezes it into an execution-specific Grant.
func (r *Registry) ResolveModelSelection(providerID, modelID, reasoningLevel string) (domainmodel.ModelSelection, error) {
	selection, err := domainmodel.NewModelSelection(providerID, modelID, reasoningLevel)
	if err != nil {
		return domainmodel.ModelSelection{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	model, err := r.modelLocked(selection.ProviderID, selection.ModelID)
	if err != nil {
		return domainmodel.ModelSelection{}, err
	}
	selected, err := r.selectModel(selection.ProviderID, model, selection.ReasoningLevel)
	if err != nil {
		return domainmodel.ModelSelection{}, err
	}
	return selected.Selection, nil
}

// Stream resolves a configured provider/model selection into a model stream.
func (r *Registry) Stream(providerID, modelID string) (runtimecontract.ModelStream, error) {
	selection, err := domainmodel.NewModelSelection(providerID, modelID, "")
	if err != nil {
		return nil, err
	}
	return r.StreamFor(selection)
}

// StreamFor builds a stream for the already-frozen model selection.
func (r *Registry) StreamFor(selection domainmodel.ModelSelection) (runtimecontract.ModelStream, error) {
	if err := selection.Validate(); err != nil {
		return nil, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	model, err := r.modelLocked(selection.ProviderID, selection.ModelID)
	if err != nil {
		return nil, err
	}
	selected, err := r.selectModel(selection.ProviderID, model, selection.ReasoningLevel)
	if err != nil {
		return nil, err
	}
	provider, ok := r.byProvider[selected.Selection.ProviderID]
	if !ok {
		return nil, fmt.Errorf("provider %q is not configured", selection.ProviderID)
	}
	client := r.providerClientLocked(provider.ID)
	key := providerKey(provider)
	if key == "" {
		return nil, fmt.Errorf("API key is not configured for provider %q", provider.ID)
	}
	apiFormat := model.APIFormat
	switch apiFormat {
	case APIFormatAnthropicMessages:
		return anthropic.New(anthropic.Config{
			BaseURL: provider.BaseURL, APIKey: key, HTTPClient: client,
		})
	case APIFormatOpenAIResponses, APIFormatOpenAIChatCompletions:
		protocol := openaicompat.ProtocolResponses
		if apiFormat == APIFormatOpenAIChatCompletions {
			protocol = openaicompat.ProtocolChatCompletions
		}
		return openaicompat.New(openaicompat.Config{
			BaseURL: provider.BaseURL, APIKey: key, HTTPClient: client,
			Protocol: protocol,
		})
	default:
		return nil, fmt.Errorf("model %q has unsupported API format %q", model.ID, apiFormat)
	}
}

func (r *Registry) selectModel(providerID string, config ModelConfig, reasoningLevel string) (domainmodel.SelectedModel, error) {
	model, err := config.DomainModel()
	if err != nil {
		return domainmodel.SelectedModel{}, err
	}
	return model.Select(providerID, reasoningLevel)
}

func providerKey(provider ProviderConfig) string {
	if provider.Credential == nil || provider.Credential.Type != CredentialTypeAPIKey {
		return ""
	}
	return strings.TrimSpace(provider.Credential.Key)
}
