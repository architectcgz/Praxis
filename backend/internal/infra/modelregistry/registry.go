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
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/infra/providers/httpclient"
	"slices"
	"strings"
	"sync"
)

const maxProviderCatalogBytes = 1 << 20

// Registry 管理可编辑的模型配置与凭据，锁只保护管理端的并发查询和保存。
// 已加载的模型文档交给业务目录处理；执行路径不依赖此可变对象。
type Registry struct {
	mu              sync.RWMutex
	modelsPath      string
	credentialsPath string
	config          modelconfig.Config
	credentials     ProviderCredentials
	client          *http.Client
	providerClients map[string]*http.Client
	providersByID   map[string]modelconfig.Provider
}

func (r *Registry) providerClientLocked(providerID string) *http.Client {
	if client := r.providerClients[providerID]; client != nil {
		return client
	}
	return httpclient.RequestClient(r.client)
}

// Config 返回编辑配置的独立副本，读取方不能修改注册表内部状态。
func (r *Registry) Config() modelconfig.Config {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.config.Clone()
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
