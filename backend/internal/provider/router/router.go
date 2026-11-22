package router

import (
	"context"
	"fmt"
	"net/http"
	"praxis/internal/model"
	"praxis/internal/provider"
	"praxis/internal/provider/anthropic"
	"praxis/internal/provider/httpclient"
	"praxis/internal/provider/openai_chat"
	"praxis/internal/provider/openai_responses"
	"sync"
)

// Credentials 提供会话已冻结的凭据，不读取运行期间的配置编辑状态。
type Credentials interface {
	ProviderKey(string) string
}

type Dependencies struct {
	Credentials Credentials
	HTTPClient  *http.Client
}

// Router 在同一会话内并发复用；只共享固定凭据和 HTTP 连接，不保存请求或解码状态。
type Router struct {
	credentials Credentials
	client      *http.Client
	mu          sync.Mutex
	clients     map[string]*http.Client
}

// New 保存装配层提供的共享依赖；具体协议及凭据由每次请求的冻结配置确定。
func New(deps Dependencies) *Router {
	return &Router{
		credentials: deps.Credentials,
		client:      deps.HTTPClient,
		clients:     make(map[string]*http.Client),
	}
}

var _ provider.Provider = (*Router)(nil)

// Stream 选择本次请求的协议并发起模型调用，事件通道和解码状态仅属于本次调用。
func (r *Router) Stream(ctx context.Context, request provider.Request) (<-chan provider.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config := request.Model
	apiKey := r.credentials.ProviderKey(config.ProviderID)
	if apiKey == "" {
		return nil, fmt.Errorf("API key is not configured for provider %q", config.ProviderID)
	}
	client, err := r.proxyClient(config.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("create provider client: %w", err)
	}
	var protocol provider.Provider
	switch model.APIFormat(config.APIFormat) {
	case model.APIFormatAnthropicMessages:
		protocol, err = anthropicmessages.New(anthropicmessages.Config{
			BaseURL:       config.BaseURL,
			APIKey:        apiKey,
			ContextWindow: config.ContextWindow,
		}, anthropicmessages.Dependencies{HTTPClient: client})
	case model.APIFormatOpenAIChatCompletions:
		protocol, err = openaichat.New(openaichat.Config{
			BaseURL:       config.BaseURL,
			APIKey:        apiKey,
			ContextWindow: config.ContextWindow,
		}, openaichat.Dependencies{HTTPClient: client})
	case model.APIFormatOpenAIResponses:
		protocol, err = openairesponses.New(openairesponses.Config{
			BaseURL:       config.BaseURL,
			APIKey:        apiKey,
			ContextWindow: config.ContextWindow,
		}, openairesponses.Dependencies{HTTPClient: client})
	default:
		return nil, fmt.Errorf("unsupported model API format %q", config.APIFormat)
	}
	if err != nil {
		return nil, err
	}
	return protocol.Stream(ctx, request)
}

// proxyClient 按冻结的代理地址复用 HTTP client，后续配置变更不会改写已有连接池。
func (r *Router) proxyClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return r.client, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if client := r.clients[proxyURL]; client != nil {
		return client, nil
	}
	client, err := httpclient.NewProxyClient(r.client, proxyURL)
	if err != nil {
		return nil, err
	}
	r.clients[proxyURL] = client
	return client, nil
}
