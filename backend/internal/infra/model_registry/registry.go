// Package modelregistry 加载用户模型配置和凭据，并提供统一的模型构建能力。
package modelregistry

import (
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"

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
)

const maxProviderCatalogBytes = 1 << 20

// StreamFactory 根据已校验的配置创建一次执行使用的模型流。
// API Key 只在注册表内部传入该工厂，不会进入配置返回值或 runtime contract。
type StreamFactory func(
	format modelconfig.APIFormat,
	provider modelconfig.Provider,
	configured modelconfig.Model,
	apiKey string,
	client *http.Client,
) (model.ModelStream, error)

// Registry is the single in-process owner of the model configuration. Saving
// from the UI replaces the config and its indexes in place, so every read path
// must hold mu; holders of this pointer never observe the swap.
type Registry struct {
	mu              sync.RWMutex
	modelsPath      string
	credentialsPath string
	config          modelconfig.Config
	credentials     ProviderCredentials
	client          *http.Client
	streamFactory   StreamFactory
	providerClients map[string]*http.Client
	modelsByKey     map[modelKey]modelconfig.Model
	providersByID   map[string]modelconfig.Provider
}

func (r *Registry) providerClientLocked(providerID string) *http.Client {
	if client := r.providerClients[providerID]; client != nil {
		return client
	}
	return providerhttp.RequestClient(r.client)
}

// Config 返回当前配置的独立副本，读取方不能修改注册表内部状态。
func (r *Registry) Config() modelconfig.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config.Clone()
}

// ReplaceFrom 用已校验的磁盘快照替换运行时配置与凭据；运行中的 Task 保持原快照。
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
func (r *Registry) modelByKeyLocked(key modelKey) (modelconfig.Model, error) {
	config, ok := r.modelsByKey[key]
	if !ok {
		return modelconfig.Model{}, fmt.Errorf("model %q for provider %q is not configured", key.ModelID, key.ProviderID)
	}
	return config, nil
}

// ListModels 返回可用于下一 Task 的模型及推理等级。
func (r *Registry) ListModels() []modelconfig.Option {
	r.mu.RLock()
	defer r.mu.RUnlock()
	options := make([]modelconfig.Option, 0)
	for _, provider := range r.config.Providers {
		for _, candidate := range provider.Models {
			options = append(options, modelconfig.Option{
				GroupID:               candidate.GroupID,
				ProviderID:            provider.ID,
				ModelID:               candidate.ID,
				DefaultProviderID:     r.config.DefaultProviderID,
				DefaultModelID:        provider.DefaultModelID,
				Label:                 candidate.DisplayName,
				ProviderName:          provider.DisplayName,
				ReasoningLevels:       slices.Clone(candidate.ReasoningLevels),
				DefaultReasoningLevel: candidate.DefaultReasoningLevel,
			})
		}
	}
	return orderModelOptions(options, r.config.Groups)
}

func orderModelOptions(options []modelconfig.Option, groups []modelconfig.Group) []modelconfig.Option {
	groupOrder := make(map[string]int, len(groups))
	for index, group := range groups {
		groupOrder[group.ID] = index
	}
	slices.SortStableFunc(options, func(left, right modelconfig.Option) int {
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
	if key == "" {
		return nil, fmt.Errorf("API key is not configured for provider %q", providerID)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, provider.BaseURL+"/v1/models", nil)
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

// FreezeTaskModel 校验当前配置并冻结非敏感模型参数，失败时不生成快照。
func (r *Registry) FreezeTaskModel(providerID, modelID, reasoningLevel string) (model.ModelSnapshot, error) {
	selection, err := newModelSelection(providerID, modelID, reasoningLevel)
	if err != nil {
		return model.ModelSnapshot{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	configured, selected, err := r.selectModelLocked(selection)
	if err != nil {
		return model.ModelSnapshot{}, err
	}
	provider, ok := r.providersByID[selected.ProviderID]
	if !ok {
		return model.ModelSnapshot{}, fmt.Errorf("provider %q is not configured", selected.ProviderID)
	}
	snapshot := model.ModelSnapshot{
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
		return model.ModelSnapshot{}, err
	}
	return snapshot, nil
}

func (r *Registry) selectModelLocked(selection modelSelection) (modelconfig.Model, modelSelection, error) {
	config, err := r.modelByKeyLocked(modelKey{ProviderID: selection.ProviderID, ModelID: selection.ModelID})
	if err != nil {
		return modelconfig.Model{}, modelSelection{}, err
	}
	selected, err := selectReasoning(config, selection.ProviderID, selection.ReasoningLevel)
	if err != nil {
		return modelconfig.Model{}, modelSelection{}, err
	}
	return config, selected, nil
}
