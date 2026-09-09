package openaicompat

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"praxis/internal/providers"
	"praxis/internal/providers/streaming"
	runtimecontract "praxis/internal/runtime"
)

type Protocol string

const (
	ProtocolChatCompletions Protocol = "chat_completions"
	ProtocolResponses       Protocol = "responses"
)

type Config struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Protocol   Protocol
}

type Provider struct {
	baseURL  string
	apiKey   string
	client   *http.Client
	protocol protocolAdapter
}

func New(config Config) (*Provider, error) {
	baseURL, err := providers.ValidateBaseURL(config.BaseURL)
	if err != nil {
		return nil, fmt.Errorf("openai-compatible: %w", err)
	}
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, errors.New("openai-compatible API key is required")
	}
	protocol := config.Protocol
	if protocol == "" {
		protocol = ProtocolChatCompletions
	}
	adapter, err := protocolFor(protocol)
	if err != nil {
		return nil, fmt.Errorf("unsupported openai-compatible protocol %q", protocol)
	}
	return &Provider{
		baseURL: baseURL, apiKey: apiKey,
		client: providers.RequestClient(config.HTTPClient), protocol: adapter,
	}, nil
}

var _ runtimecontract.ModelStream = (*Provider)(nil)

func (p *Provider) Stream(
	ctx context.Context,
	request runtimecontract.ModelRequest,
) (<-chan runtimecontract.ModelStreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("openai-compatible stream context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, path, err := p.requestPayload(request.Snapshot)
	if err != nil {
		return nil, err
	}
	httpRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+path, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("build openai-compatible request: %w", err)
	}
	httpRequest.Header.Set("Authorization", "Bearer "+p.apiKey)
	httpRequest.Header.Set("Content-Type", "application/json")
	httpRequest.Header.Set("Accept", "text/event-stream")
	response, err := streaming.Open(p.client, httpRequest)
	if err != nil {
		return nil, fmt.Errorf("openai-compatible request: %w", err)
	}
	return streaming.Start(ctx, response, p.protocol), nil
}

func (p *Provider) requestPayload(snapshot runtimecontract.ExecutionTurnSnapshot) ([]byte, string, error) {
	return p.protocol.requestPayload(snapshot)
}

func chatMessage(message runtimecontract.TurnMessage) map[string]any {
	result := map[string]any{"role": string(message.Role)}
	var text strings.Builder
	toolCalls := make([]map[string]any, 0)
	for _, block := range message.Content {
		switch block.Kind {
		case runtimecontract.TurnContentText:
			text.WriteString(block.Text)
		case runtimecontract.TurnContentToolUse:
			toolCalls = append(toolCalls, map[string]any{
				"id":   block.ToolCallID,
				"type": "function",
				"function": map[string]string{
					"name": block.ToolName, "arguments": string(block.Input),
				},
			})
		case runtimecontract.TurnContentToolResult:
			result["role"] = "tool"
			result["tool_call_id"] = block.ToolCallID
			text.WriteString(block.Text)
		}
	}
	if len(toolCalls) > 0 {
		result["tool_calls"] = toolCalls
	}
	result["content"] = text.String()
	return result
}

func responseItems(message runtimecontract.TurnMessage) []map[string]any {
	items := make([]map[string]any, 0, len(message.Content))
	var text strings.Builder
	flushText := func() {
		if text.Len() == 0 {
			return
		}
		items = append(items, map[string]any{
			"type": "message", "role": string(message.Role), "content": text.String(),
		})
		text.Reset()
	}
	for _, block := range message.Content {
		switch block.Kind {
		case runtimecontract.TurnContentText:
			text.WriteString(block.Text)
		case runtimecontract.TurnContentToolUse:
			flushText()
			items = append(items, map[string]any{
				"type": "function_call", "call_id": block.ToolCallID,
				"name": block.ToolName, "arguments": string(block.Input),
			})
		case runtimecontract.TurnContentToolResult:
			flushText()
			items = append(items, map[string]any{
				"type": "function_call_output", "call_id": block.ToolCallID, "output": block.Text,
			})
		}
	}
	flushText()
	return items
}
