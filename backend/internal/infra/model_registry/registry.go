// Package modelregistry loads the user-owned model configuration and auth
// documents and materializes provider-neutral model interfaces.
package modelregistry

import (
	"praxis/internal/contracts"

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	providerhttp "praxis/internal/infra/providers"
	"slices"
	"strings"
	"sync"

	runtimecontract "praxis/internal/agent_runtime"
)

const maxProviderCatalogBytes = 1 << 20

// StreamFactory 根据已校验的配置创建一次执行使用的模型流。
// API Key 只在注册表内部传入该工厂，不会进入配置返回值或 runtime contract。
type StreamFactory func(
	format ModelAPIFormat,
	provider ProviderConfig,
	model ModelConfig,
	apiKey string,
	client *http.Client,
) (runtimecontract.ModelStream, error)

// Registry is the single in-process owner of the model configuration. Saving
// from the UI replaces the config and its indexes in place, so every read path
// must hold mu; holders of this pointer never observe the swap.
type Registry struct {
	mu              sync.RWMutex
	modelsPath      string
	credentialsPath string
	config          RegistryConfig
	credentials     ProviderCredentials
	client          *http.Client
	streamFactory   StreamFactory
	providerClients map[string]*http.Client
	modelsByKey     map[modelKey]ModelConfig
	providersByID   map[string]ProviderConfig
}

func (r *Registry) providerClientLocked(providerID string) *http.Client {
	if client := r.providerClients[providerID]; client != nil {
		return client
	}
	return providerhttp.RequestClient(r.client)
}

// Config returns a deep copy of the current configuration for the settings UI.
func (r *Registry) Config() RegistryConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return cloneRegistryConfig(r.config)
}

// ReplaceFrom 用已校验的磁盘快照替换运行时配置与凭据；运行中的 Turn 保持原快照。
func (r *Registry) ReplaceFrom(candidate *Registry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = candidate.config
	r.credentials = candidate.credentials
	r.providerClients = candidate.providerClients
	r.modelsByKey = candidate.modelsByKey
	r.providersByID = candidate.providersByID
}

// modelByKeyLocked 要求调用方已持有 r.mu，且 key 已完成规范化。
func (r *Registry) modelByKeyLocked(key modelKey) (ModelConfig, error) {
	config, ok := r.modelsByKey[key]
	if !ok {
		return ModelConfig{}, fmt.Errorf("model %q for provider %q is not configured", key.ModelID, key.ProviderID)
	}
	return config, nil
}

// ListModels 返回可用于下一 Turn 的模型及推理等级。
func (r *Registry) ListModels() []ModelOption {
	r.mu.RLock()
	defer r.mu.RUnlock()
	options := make([]ModelOption, 0)
	for _, provider := range r.config.Providers {
		for _, candidate := range provider.Models {
			label := strings.TrimSpace(candidate.DisplayName)
			if label == "" {
				label = candidate.ID
			}
			providerName := strings.TrimSpace(provider.DisplayName)
			if providerName == "" {
				providerName = provider.BaseURL
			}
			options = append(options, ModelOption{
				GroupID: candidate.GroupID, ProviderID: provider.ID, ModelID: candidate.ID,
				DefaultProviderID: r.config.DefaultProviderID, DefaultModelID: provider.DefaultModelID,
				Label: label, ProviderName: providerName,
				ReasoningLevels:       append([]string(nil), candidate.ReasoningLevels...),
				DefaultReasoningLevel: candidate.DefaultReasoningLevel,
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
	slices.SortStableFunc(options, func(left, right ModelOption) int {
		leftGroup, leftOK := groupOrder[left.GroupID]
		rightGroup, rightOK := groupOrder[right.GroupID]
		if leftOK && rightOK && leftGroup != rightGroup {
			return leftGroup - rightGroup
		}
		if leftOK != rightOK {
			if leftOK {
				return -1
			}
			return 1
		}
		if left.Label == right.Label {
			if left.ProviderID == right.ProviderID {
				return strings.Compare(left.ModelID, right.ModelID)
			}
			return strings.Compare(left.ProviderID, right.ProviderID)
		}
		return strings.Compare(left.Label, right.Label)
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
	r.mu.RLock()
	provider, exists := r.providersByID[providerID]
	client := r.providerClientLocked(providerID)
	key := r.credentialKeyLocked(providerID)
	r.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("provider %q is not configured", providerID)
	}
	baseURL, err := providerhttp.ValidateBaseURL(provider.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("provider %q: %w", providerID, err)
	}
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
		return nil, providerhttp.DecodeErrorResponse(response)
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
	slices.Sort(models)
	return models, nil
}

// FreezeTurnModel 校验当前配置并冻结非敏感模型参数，失败时不生成快照。
func (r *Registry) FreezeTurnModel(providerID, modelID, reasoningLevel string) (contracts.ModelSnapshot, error) {
	selection, err := newModelSelection(providerID, modelID, reasoningLevel)
	if err != nil {
		return contracts.ModelSnapshot{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	configured, selected, err := r.selectModelLocked(selection)
	if err != nil {
		return contracts.ModelSnapshot{}, err
	}
	provider, ok := r.providersByID[selected.ProviderID]
	if !ok {
		return contracts.ModelSnapshot{}, fmt.Errorf("provider %q is not configured", selected.ProviderID)
	}
	snapshot := contracts.ModelSnapshot{
		ProviderID:      selected.ProviderID,
		ModelID:         selected.ModelID,
		ReasoningLevel:  selected.ReasoningLevel,
		APIFormat:       string(configured.APIFormat),
		ContextWindow:   configured.ContextWindow,
		MaxOutputTokens: configured.MaxOutputTokens,
		BaseURL:         provider.BaseURL,
		ProxyURL:        provider.ProxyURL,
	}
	if err := snapshot.Validate(); err != nil {
		return contracts.ModelSnapshot{}, err
	}
	return snapshot, nil
}

func (r *Registry) selectModelLocked(selection modelSelection) (ModelConfig, modelSelection, error) {
	config, err := r.modelByKeyLocked(modelKey{ProviderID: selection.ProviderID, ModelID: selection.ModelID})
	if err != nil {
		return ModelConfig{}, modelSelection{}, err
	}
	selected, err := config.selectReasoning(selection.ProviderID, selection.ReasoningLevel)
	if err != nil {
		return ModelConfig{}, modelSelection{}, err
	}
	return config, selected, nil
}
