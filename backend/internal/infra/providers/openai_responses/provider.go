package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	appcontext "praxis/internal/core/context"
	providerapi "praxis/internal/core/provider"
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

// New 使用入口已校验的端点创建 OpenAI Responses 协议适配器，并检查凭据。
func New(config Config, deps Dependencies) (*Provider, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai responses API key is required")
	}
	return &Provider{
		baseURL:       config.BaseURL,
		apiKey:        apiKey,
		client:        deps.HTTPClient,
		contextWindow: config.ContextWindow,
	}, nil
}

var _ providerapi.Provider = (*Provider)(nil)

// Stream 将一次 provider-neutral 请求编码为 Responses 流并返回事件通道。
func (p *Provider) Stream(
	ctx context.Context,
	request providerapi.Request,
) (<-chan providerapi.Event, error) {
	if ctx == nil {
		return nil, errors.New("openai responses stream context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, inputItems, err := buildPayload(request)
	if err != nil {
		return nil, err
	}
	if err := appcontext.ExceedsContextWindow(payload, request.MaxOutputTokens, p.contextWindow); err != nil {
		return nil, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/v1/responses", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build openai responses request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	response, err := streaming.Open(p.client, httpRequest)
	if err != nil {
		return nil, fmt.Errorf(
			"openai responses request (inputItems=%d payloadBytes=%d): %w",
			inputItems, len(payload), streaming.RedactError(err, sensitiveStateValues(request.Context)),
		)
	}
	return streaming.Start(ctx, response, decoder{}, sensitiveStateValues(request.Context)...), nil
}

func buildPayload(request providerapi.Request) ([]byte, int, error) {
	if request.Model.ModelID == "" {
		return nil, 0, errors.New("openai responses model is required")
	}
	if request.MaxOutputTokens <= 0 {
		return nil, 0, errors.New("openai responses max output tokens is required")
	}
	input, err := encodeContext(request.Context, request.Model)
	if err != nil {
		return nil, 0, err
	}
	payload := requestPayload{
		Model:           request.Model.ModelID,
		Input:           input,
		Stream:          true,
		Store:           false,
		Include:         []string{"reasoning.encrypted_content"},
		MaxOutputTokens: request.MaxOutputTokens,
		// 同一会话的多轮请求共用路由 key，不能使用每轮变化的 Task ID 或上下文摘要。
		PromptCacheKey: request.PromptCacheKey,
	}
	if schema := request.OutputSchema; schema != nil {
		if err := schema.Validate(); err != nil {
			return nil, 0, fmt.Errorf("openai responses output schema: %w", err)
		}
		payload.Text = &responseTextConfig{
			Format: responseJSONSchema{
				Type:   "json_schema",
				Name:   schema.Name,
				Schema: schema.Schema,
				Strict: true,
			},
		}
	}
	if reasoning := request.Model.ReasoningLevel; reasoning != "" && reasoning != "off" {
		payload.Reasoning = &reasoningConfig{Effort: reasoning, Summary: "auto"}
	}
	if len(request.Tools) > 0 {
		payload.Tools = make([]responseTool, 0, len(request.Tools))
		for _, tool := range request.Tools {
			payload.Tools = append(payload.Tools, responseTool{
				Type:        "function",
				Name:        string(tool.Name),
				Description: tool.Description,
				Parameters:  json.RawMessage(tool.InputSchema),
			})
		}
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, fmt.Errorf("encode openai responses request: %w", err)
	}
	if len(input) == 0 {
		return nil, 0, errors.New("openai responses input is required")
	}
	return encoded, len(input), nil
}
