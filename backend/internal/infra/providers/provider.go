package providers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	modelconfig "praxis/internal/core/model/config"
	providerapi "praxis/internal/core/provider"
	anthropicmessages "praxis/internal/infra/providers/anthropic"
	"praxis/internal/infra/providers/httpclient"
	openaichat "praxis/internal/infra/providers/openai_chat"
	openairesponses "praxis/internal/infra/providers/openai_responses"
	"sync"
)

// Credentials 在请求发起时读取当前凭据，不改变请求中已经冻结的模型配置。
type Credentials interface {
	ProviderKey(string) string
}

type Dependencies struct {
	Credentials Credentials
	HTTPClient  *http.Client
}

// Provider 可由多次执行并发复用；只共享凭据来源和 HTTP 连接，不保存请求或解码状态。
type Provider struct {
	credentials Credentials
	client      *http.Client
	mu          sync.Mutex
	clients     map[string]*http.Client
}

// New 在装配时校验共享依赖；具体协议及凭据由每次请求的冻结配置确定。
func New(deps Dependencies) (*Provider, error) {
	if deps.Credentials == nil {
		return nil, errors.New("provider credentials are required")
	}
	return &Provider{
		credentials: deps.Credentials,
		client:      httpclient.RequestClient(deps.HTTPClient),
		clients:     make(map[string]*http.Client),
	}, nil
}

var _ providerapi.Provider = (*Provider)(nil)

// Stream 选择本次请求的协议并发起模型调用，事件通道和解码状态仅属于本次调用。
func (p *Provider) Stream(ctx context.Context, request providerapi.Request) (<-chan providerapi.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	config := request.Model
	apiKey := p.credentials.ProviderKey(config.ProviderID)
	if apiKey == "" {
		return nil, fmt.Errorf("API key is not configured for provider %q", config.ProviderID)
	}
	client, err := p.proxyClient(config.ProxyURL)
	if err != nil {
		return nil, fmt.Errorf("create provider client: %w", err)
	}
	var protocol providerapi.Provider
	switch modelconfig.APIFormat(config.APIFormat) {
	case modelconfig.APIFormatAnthropicMessages:
		protocol, err = anthropicmessages.New(anthropicmessages.Config{
			BaseURL:       config.BaseURL,
			APIKey:        apiKey,
			ContextWindow: config.ContextWindow,
		}, anthropicmessages.Dependencies{HTTPClient: client})
	case modelconfig.APIFormatOpenAIChatCompletions:
		protocol, err = openaichat.New(openaichat.Config{
			BaseURL:       config.BaseURL,
			APIKey:        apiKey,
			ContextWindow: config.ContextWindow,
		}, openaichat.Dependencies{HTTPClient: client})
	case modelconfig.APIFormatOpenAIResponses:
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
func (p *Provider) proxyClient(proxyURL string) (*http.Client, error) {
	if proxyURL == "" {
		return p.client, nil
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if client := p.clients[proxyURL]; client != nil {
		return client, nil
	}
	client, err := httpclient.NewProxyClient(p.client, proxyURL)
	if err != nil {
		return nil, err
	}
	p.clients[proxyURL] = client
	return client, nil
}
