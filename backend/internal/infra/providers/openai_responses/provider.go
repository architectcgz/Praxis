package openairesponses

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	runtimecontract "praxis/internal/agent_runtime"
	appcontext "praxis/internal/core/context"
	"praxis/internal/infra/providers"
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
	baseURL, err := providers.ValidateBaseURL(config.BaseURL)
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

var _ runtimecontract.ModelStream = (*Provider)(nil)

// Stream 将一次 provider-neutral 请求编码为 Responses 流并返回事件通道。
func (p *Provider) Stream(
	ctx context.Context,
	request runtimecontract.ModelRequest,
) (<-chan runtimecontract.ModelStreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("openai responses stream context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, path, err := p.requestPayload(request)
	if err != nil {
		return nil, err
	}
	if err := appcontext.ExceedsContextWindow(payload, request.MaxOutputTokens, p.contextWindow); err != nil {
		return nil, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(payload))
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
			requestInputItemCount(request.Context), len(payload), err,
		)
	}
	return streaming.Start(ctx, response, streamDecoder{}), nil
}

func (p *Provider) requestPayload(request runtimecontract.ModelRequest) ([]byte, string, error) {
	return encodeRequest(request)
}

func encodeRequest(request runtimecontract.ModelRequest) ([]byte, string, error) {
	if request.Model.ModelID == "" {
		return nil, "", errors.New("openai responses model is required")
	}
	if request.MaxOutputTokens <= 0 {
		return nil, "", errors.New("openai responses max output tokens is required")
	}
	input := make([]responseItem, 0, len(request.Context.Entries)+1)
	if request.Context.SystemPrompt != "" {
		input = append(input, responseItem{
			Type: "message",
			Role: "system",
			Content: []responseContentPart{{
				Type: "input_text",
				Text: request.Context.SystemPrompt,
			}},
		})
	}
	for _, entry := range request.Context.Entries {
		input = append(input, responseItems(entry)...)
	}
	payload := responseRequest{
		Model:           request.Model.ModelID,
		Input:           input,
		Stream:          true,
		MaxOutputTokens: request.MaxOutputTokens,
		// 同一会话的多轮请求共用路由 key，不能使用每轮变化的 Turn ID 或上下文摘要。
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
		return nil, "", fmt.Errorf("encode openai responses request: %w", err)
	}
	if requestInputItemCount(request.Context) == 0 {
		return nil, "", errors.New("openai responses input is required")
	}
	return encoded, "/v1/responses", nil
}

func requestInputItemCount(modelContext appcontext.ModelContext) int {
	count := 0
	for _, entry := range modelContext.Entries {
		count += len(responseItems(entry))
	}
	if modelContext.SystemPrompt != "" {
		count++
	}
	return count
}

type responseRequest struct {
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

type responseItem struct {
	Type      string                `json:"type"`
	Role      string                `json:"role,omitempty"`
	Content   []responseContentPart `json:"content,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	Output    string                `json:"output,omitempty"`
}

type responseContentPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type responseTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
