package openaichat

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

// New 创建 OpenAI Chat Completions 协议适配器。
func New(config Config) (*Provider, error) {
	baseURL, err := providers.ValidateBaseURL(config.BaseURL)
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

var _ runtimecontract.ModelStream = (*Provider)(nil)

// Stream 将一次 provider-neutral 请求编码为 Chat Completions 流并返回事件通道。
func (p *Provider) Stream(
	ctx context.Context,
	request runtimecontract.ModelRequest,
) (<-chan runtimecontract.ModelStreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("openai chat stream context is required")
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
		return nil, fmt.Errorf("build openai chat request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	response, err := streaming.Open(p.client, httpRequest)
	if err != nil {
		return nil, fmt.Errorf("openai chat request: %w", err)
	}
	return streaming.Start(ctx, response, streamDecoder{}), nil
}

func (p *Provider) requestPayload(request runtimecontract.ModelRequest) ([]byte, string, error) {
	return encodeRequest(request)
}

func encodeRequest(request runtimecontract.ModelRequest) ([]byte, string, error) {
	if request.Model.ModelID == "" {
		return nil, "", errors.New("openai chat model is required")
	}
	if request.MaxOutputTokens <= 0 {
		return nil, "", errors.New("openai chat max output tokens is required")
	}
	messages := make([]chatMessage, 0, len(request.Context.Entries)+1)
	if request.Context.SystemPrompt != "" {
		messages = append(messages, chatMessage{
			Role:    "system",
			Content: request.Context.SystemPrompt,
		})
	}
	for _, entry := range request.Context.Entries {
		messages = append(messages, chatMessageFromContext(entry))
	}
	payload := chatRequest{
		Model:    request.Model.ModelID,
		Messages: messages,
		Stream:   true,
		StreamOptions: chatStreamOptions{
			IncludeUsage: true,
		},
		MaxTokens: request.MaxOutputTokens,
		// 同一会话的多轮请求共用路由 key，不能使用每轮变化的 Turn ID 或上下文摘要。
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
		return nil, "", fmt.Errorf("encode openai chat request: %w", err)
	}
	return encoded, "/v1/chat/completions", nil
}

type chatRequest struct {
	Model           string            `json:"model"`
	Messages        []chatMessage     `json:"messages"`
	Stream          bool              `json:"stream"`
	StreamOptions   chatStreamOptions `json:"stream_options"`
	ReasoningEffort string            `json:"reasoning_effort,omitempty"`
	MaxTokens       int               `json:"max_tokens"`
	Tools           []chatTool        `json:"tools,omitempty"`
	PromptCacheKey  string            `json:"prompt_cache_key,omitempty"`
}

type chatStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type chatMessage struct {
	Role       string         `json:"role"`
	Content    string         `json:"content"`
	ToolCalls  []chatToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
}

type chatToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"`
	Function chatFunctionCall `json:"function"`
}

type chatFunctionCall struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type chatTool struct {
	Type     string                 `json:"type"`
	Function chatFunctionDefinition `json:"function"`
}

type chatFunctionDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}
