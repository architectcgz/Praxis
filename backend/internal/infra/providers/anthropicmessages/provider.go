package anthropicmessages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/providers/streaming"
)

type Config struct {
	BaseURL       string
	APIKey        string
	HTTPClient    *http.Client
	Version       string
	ContextWindow int
}

type Provider struct {
	baseURL, apiKey, version string
	client                   *http.Client
	contextWindow            int
}

// New 创建 Anthropic Messages 协议适配器；校验连接配置并补齐默认协议版本。
func New(config Config) (*Provider, error) {
	baseURL, err := modelconfig.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("anthropic: %w", err)
	}
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, errors.New("anthropic API key is required")
	}
	version := strings.TrimSpace(config.Version)
	if version == "" {
		version = "2023-06-01"
	}
	return &Provider{
		baseURL:       baseURL,
		apiKey:        apiKey,
		version:       version,
		client:        config.HTTPClient,
		contextWindow: config.ContextWindow,
	}, nil
}

var _ model.ModelStream = (*Provider)(nil)

// Stream 组装完整模型请求并返回统一事件通道；请求无效或连接失败时返回错误。
// 调用方提前停止消费时必须取消 ctx，以释放流资源。
func (p *Provider) Stream(
	ctx context.Context,
	request model.ModelRequest,
) (<-chan model.ModelStreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("anthropic stream context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := buildPayload(request)
	if err != nil {
		return nil, err
	}
	if err := appcontext.ExceedsContextWindow(payload, request.MaxOutputTokens, p.contextWindow); err != nil {
		return nil, err
	}
	httpRequest, err := http.NewRequestWithContext(
		ctx, http.MethodPost, p.baseURL+"/v1/messages", bytes.NewReader(payload),
	)
	if err != nil {
		return nil, fmt.Errorf("build anthropic request: %w", err)
	}
	httpRequest.Header.Set("x-api-key", p.apiKey)
	httpRequest.Header.Set("anthropic-version", p.version)
	httpRequest.Header.Set("content-type", "application/json")
	httpRequest.Header.Set("accept", "text/event-stream")
	response, err := streaming.Open(providers.RequestClient(p.client), httpRequest)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	return streaming.Start(ctx, response, decoder{}), nil
}

func buildPayload(request model.ModelRequest) ([]byte, error) {
	modelID := request.Model.ModelID
	if modelID == "" {
		return nil, errors.New("anthropic model is required")
	}
	if request.MaxOutputTokens <= 0 {
		return nil, errors.New("anthropic max output tokens is required")
	}
	encodedContext, err := encodeContext(request.Context)
	if err != nil {
		return nil, err
	}
	body := map[string]any{
		"model":    modelID,
		"messages": encodedContext.Messages,
		"stream":   true,
		// 自动缓存随对话增长推进断点，复用包含工具定义和 system 的相同前缀。
		"cache_control": map[string]string{"type": "ephemeral"},
	}
	if reasoning := request.Model.ReasoningLevel; reasoning != "" && reasoning != "off" {
		body["output_config"] = map[string]string{"effort": reasoning}
	}
	body["max_tokens"] = request.MaxOutputTokens
	if encodedContext.System != "" {
		body["system"] = encodedContext.System
	}
	if len(request.Tools) > 0 {
		tools := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
			tools = append(tools, map[string]any{
				"name":         tool.Name,
				"description":  tool.Description,
				"input_schema": json.RawMessage(tool.InputSchema),
			})
		}
		body["tools"] = tools
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode anthropic request: %w", err)
	}
	return payload, nil
}
