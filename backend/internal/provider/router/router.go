package router

import (
	"context"
	"fmt"
	"maps"
	"net/http"
	"praxis/internal/model"
	"praxis/internal/provider"
	"praxis/internal/provider/anthropic"
	"praxis/internal/provider/openai_chat"
	"praxis/internal/provider/openai_responses"
	"praxis/internal/provider/streaming"
)

// Credentials 提供会话已冻结的凭据，不读取运行期间的配置编辑状态。
type Credentials interface {
	ProviderKey(string) string
}

type Dependencies struct {
	Credentials  Credentials
	HTTPClients  map[string]*http.Client
	Warnf        func(string, ...any)
	StreamLimits streaming.Limits
}

// Router 在同一会话内并发复用；只共享固定凭据和 HTTP 连接，不保存请求或解码状态。
type Router struct {
	credentials Credentials
	clients     map[string]*http.Client
	warnf       func(string, ...any)
	limits      streaming.Limits
}

// New 保存装配层提供的共享依赖；具体协议及凭据由每次请求的冻结配置确定。
func New(deps Dependencies) *Router {
	return &Router{
		credentials: deps.Credentials,
		clients:     deps.HTTPClients,
		warnf:       deps.Warnf,
		limits:      deps.StreamLimits,
	}
}

var _ provider.Provider = (*Router)(nil)

func (r *Router) CloseIdleConnections() {
	for client := range maps.Values(r.clients) {
		client.CloseIdleConnections()
	}
}

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
	client := r.clients[config.ProviderID]
	var protocol provider.Provider
	switch model.APIFormat(config.APIFormat) {
	case model.APIFormatAnthropicMessages:
		protocol = anthropicmessages.New(anthropicmessages.Config{
			BaseURL: config.BaseURL,
			APIKey:  apiKey,
		}, anthropicmessages.Dependencies{
			HTTPClient:   client,
			Warnf:        r.warnf,
			StreamLimits: r.limits,
		})
	case model.APIFormatOpenAIChatCompletions:
		protocol = openaichat.New(openaichat.Config{
			BaseURL: config.BaseURL,
			APIKey:  apiKey,
		}, openaichat.Dependencies{
			HTTPClient:   client,
			Warnf:        r.warnf,
			StreamLimits: r.limits,
		})
	case model.APIFormatOpenAIResponses:
		protocol = openairesponses.New(openairesponses.Config{
			BaseURL: config.BaseURL,
			APIKey:  apiKey,
		}, openairesponses.Dependencies{
			HTTPClient:   client,
			Warnf:        r.warnf,
			StreamLimits: r.limits,
		})
	default:
		return nil, fmt.Errorf("unsupported model API format %q", config.APIFormat)
	}
	return protocol.Stream(ctx, request)
}
