package openaichat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"praxis/internal/provider"
	"praxis/internal/provider/streaming"
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
	baseURL string
	apiKey  string
	client  *http.Client
}

// New 使用入口已校验的端点和凭据创建 OpenAI Chat Completions 协议适配器。
func New(config Config, deps Dependencies) (*Provider, error) {
	return &Provider{
		baseURL: config.BaseURL,
		apiKey:  config.APIKey,
		client:  deps.HTTPClient,
	}, nil
}

var _ provider.Provider = (*Provider)(nil)

// Stream 将一次 provider-neutral 请求编码为 Chat Completions 流并返回事件通道。
func (p *Provider) Stream(
	ctx context.Context,
	request provider.Request,
) (<-chan provider.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := buildPayload(request)
	if err != nil {
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

func buildPayload(request provider.Request) ([]byte, error) {
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
		// 同一会话的多轮请求共用路由 key，不能使用每次变化的 Run ID、Request ID 或上下文摘要。
		PromptCacheKey: request.PromptCacheKey,
	}
	if schema := request.OutputSchema; schema != nil {
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
