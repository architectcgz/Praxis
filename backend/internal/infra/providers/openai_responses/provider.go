package openairesponses

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

// New 创建 OpenAI Responses 协议适配器。
func New(config Config) (*Provider, error) {
	baseURL, err := modelconfig.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("openai responses: %w", err)
	}
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai responses API key is required")
	}
	return &Provider{
		baseURL:       baseURL,
		apiKey:        apiKey,
		client:        config.HTTPClient,
		contextWindow: config.ContextWindow,
	}, nil
}

var _ model.ModelStream = (*Provider)(nil)

// Stream 将一次 provider-neutral 请求编码为 Responses 流并返回事件通道。
func (p *Provider) Stream(
	ctx context.Context,
	request model.ModelRequest,
) (<-chan model.ModelStreamEvent, error) {
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
			inputItems, len(payload), err,
		)
	}
	return streaming.Start(ctx, response, decoder{}), nil
}

func buildPayload(request model.ModelRequest) ([]byte, int, error) {
	if request.Model.ModelID == "" {
		return nil, 0, errors.New("openai responses model is required")
	}
	if request.MaxOutputTokens <= 0 {
		return nil, 0, errors.New("openai responses max output tokens is required")
	}
	input, err := encodeContext(request.Context)
	if err != nil {
		return nil, 0, err
	}
	payload := requestPayload{
		Model:           request.Model.ModelID,
		Input:           input,
		Stream:          true,
		MaxOutputTokens: request.MaxOutputTokens,
		// 同一会话的多轮请求共用路由 key，不能使用每轮变化的 Task ID 或上下文摘要。
		PromptCacheKey: request.SessionReference,
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

type requestPayload struct {
	Model           string           `json:"model"`
	Input           []responseItem   `json:"input"`
	Stream          bool             `json:"stream"`
	Reasoning       *reasoningConfig `json:"reasoning,omitempty"`
	MaxOutputTokens int              `json:"max_output_tokens"`
	Tools           []responseTool   `json:"tools,omitempty"`
	PromptCacheKey  string           `json:"prompt_cache_key,omitempty"`
}

type reasoningConfig struct {
	Effort  string `json:"effort"`
	Summary string `json:"summary,omitempty"`
}

type responseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
