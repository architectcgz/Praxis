// Registry 加载并管理模型配置和凭据。
package modelregistry

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	modelapi "praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/infra/providers/httpclient"
	"slices"
	"strings"
	"sync"
)

const maxProviderCatalogBytes = 1 << 20

// Registry 在进程内统一管理模型配置。界面保存时原地替换配置及索引，
// 因此所有读取路径必须持有 mu，持有此指针的调用方不会观察到切换中的状态。
type Registry struct {
	mu              sync.RWMutex
	modelsPath      string
	credentialsPath string
	config          modelconfig.Config
	credentials     ProviderCredentials
	client          *http.Client
	providerClients map[string]*http.Client
	modelConfigs    map[modelKey]modelapi.Config
	providersByID   map[string]modelconfig.Provider
}

func (r *Registry) providerClientLocked(providerID string) *http.Client {
	if client := r.providerClients[providerID]; client != nil {
		return client
	}
	return httpclient.RequestClient(r.client)
}

// modelapi.Config 返回当前配置的独立副本，读取方不能修改注册表内部状态。
func (r *Registry) Config() modelconfig.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config.Clone()
}

// ReplaceFrom 用已校验的磁盘配置替换运行时配置与凭据；运行中的 Task 保持已选配置。
func (r *Registry) ReplaceFrom(candidate *Registry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.config = candidate.config
	r.credentials = candidate.credentials
	r.providerClients = candidate.providerClients
	r.modelConfigs = candidate.modelConfigs
	r.providersByID = candidate.providersByID
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

// DiscoverProviderModels 获取已配置的 OpenAI 兼容 Provider 公布的模型 ID。
// 凭据仅在进程内解析，不返回给调用方。
func (r *Registry) DiscoverProviderModels(ctx context.Context, providerID string) ([]string, error) {
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
		return nil, httpclient.DecodeErrorResponse(response)
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

// ModelConfig 返回显式选定模型的内存快照，只覆盖本次请求指定的推理等级。
// 完整配置在加载或保存时构建；返回独立副本，避免单次执行修改注册表状态。
func (r *Registry) ModelConfig(providerID, modelID, reasoningLevel string) (modelapi.Config, error) {
	if providerID == "" || modelID == "" {
		return modelapi.Config{}, errors.New("model provider ID and model ID are required")
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	config, ok := r.modelConfigs[modelKey{ProviderID: providerID, ModelID: modelID}]
	if !ok {
		return modelapi.Config{}, fmt.Errorf("model %q for provider %q is not configured", modelID, providerID)
	}
	if reasoningLevel != "" {
		if !slices.Contains(config.ReasoningLevels, reasoningLevel) {
			return modelapi.Config{}, fmt.Errorf("model %q does not support reasoning %q", modelID, reasoningLevel)
		}
		config.ReasoningLevel = reasoningLevel
	}
	return config.Clone(), nil
}
