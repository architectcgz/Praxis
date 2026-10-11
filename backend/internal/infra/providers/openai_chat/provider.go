package openaichat

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
	"praxis/internal/infra/providers/streaming"
)

type Config struct {
	BaseURL       string
	APIKey        string
	HTTPClient    *http.Client
	ContextWindow int
}

type Provider struct {
	baseURL       string
	apiKey        string
	client        *http.Client
	contextWindow int
}

// New 创建 OpenAI Chat Completions 协议适配器。
func New(config Config) (*Provider, error) {
	baseURL, err := modelconfig.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("openai chat: %w", err)
	}
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai chat API key is required")
	}
	return &Provider{
		baseURL:       baseURL,
		apiKey:        apiKey,
		client:        config.HTTPClient,
		contextWindow: config.ContextWindow,
	}, nil
}

var _ model.ModelStream = (*Provider)(nil)

// Stream 将一次 provider-neutral 请求编码为 Chat Completions 流并返回事件通道。
func (p *Provider) Stream(
	ctx context.Context,
	request model.ModelRequest,
) (<-chan model.ModelStreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("openai chat stream context is required")
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
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build openai chat request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	response, err := streaming.Open(p.client, httpRequest)
	if err != nil {
		return nil, fmt.Errorf("openai chat request: %w", err)
	}
	return streaming.Start(ctx, response, decoder{}), nil
}

func buildPayload(request model.ModelRequest) ([]byte, error) {
	if request.Model.ModelID == "" {
		return nil, errors.New("openai chat model is required")
	}
	if request.MaxOutputTokens <= 0 {
		return nil, errors.New("openai chat max output tokens is required")
	}
	messages, err := encodeContext(request.Context)
	if err != nil {
		return nil, err
	}
	payload := chatRequest{
		Model:    request.Model.ModelID,
		Messages: messages,
		Stream:   true,
		StreamOptions: chatStreamOptions{
			IncludeUsage: true,
		},
		MaxCompletionTokens: request.MaxOutputTokens,
		N:                   1,
		// 同一会话的多轮请求共用路由 key，不能使用每轮变化的 Task ID 或上下文摘要。
		PromptCacheKey: request.SessionReference,
	}
	if reasoning := request.Model.ReasoningLevel; reasoning != "" && reasoning != "off" {
		payload.ReasoningEffort = reasoning
	}
	if len(request.Tools) > 0 {
		payload.Tools = make([]chatTool, 0, len(request.Tools))
		for _, tool := range request.Tools {
			payload.Tools = append(payload.Tools, chatTool{
				Type: "function",
				Function: chatFunctionDefinition{
					Name:        string(tool.Name),
					Description: tool.Description,
					Parameters:  json.RawMessage(tool.InputSchema),
				},
			})
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("encode openai chat request: %w", err)
	}
	return encoded, nil
}
