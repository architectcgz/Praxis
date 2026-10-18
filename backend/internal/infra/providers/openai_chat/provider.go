package openaichat

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	appcontext "praxis/internal/core/context"
	modelapi "praxis/internal/core/model"
	"praxis/internal/infra/providers/streaming"
	"strings"
)

type Config struct {
	BaseURL       string
	APIKey        string
	ContextWindow int
}

type Dependencies struct {
	HTTPClient *http.Client
}

type Provider struct {
	baseURL       string
	apiKey        string
	client        *http.Client
	contextWindow int
}

// New 使用入口已校验的端点创建 OpenAI Chat Completions 协议适配器，并检查凭据。
func New(config Config, deps Dependencies) (*Provider, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai chat API key is required")
	}
	return &Provider{
		baseURL:       config.BaseURL,
		apiKey:        apiKey,
		client:        deps.HTTPClient,
		contextWindow: config.ContextWindow,
	}, nil
}

var _ modelapi.ModelStream = (*Provider)(nil)

// Stream 将一次 provider-neutral 请求编码为 Chat Completions 流并返回事件通道。
func (p *Provider) Stream(
	ctx context.Context,
	request modelapi.ModelRequest,
) (<-chan modelapi.ModelStreamEvent, error) {
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

func buildPayload(request modelapi.ModelRequest) ([]byte, error) {
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
		PromptCacheKey: request.PromptCacheKey,
	}
	if schema := request.OutputSchema; schema != nil {
		if err := schema.Validate(); err != nil {
			return nil, fmt.Errorf("openai chat output schema: %w", err)
		}
		payload.ResponseFormat = &chatResponseFormat{
			Type: "json_schema",
			JSONSchema: chatJSONSchema{
				Name:   schema.Name,
				Schema: schema.Schema,
				Strict: true,
			},
		}
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
