package anthropic

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"

	domainsecurity "praxis/internal/domain/security"

	"praxis/internal/providers"
	"praxis/internal/providers/streaming"
	runtimecontract "praxis/internal/runtime"
)

type Config struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Version    string
}

type Provider struct {
	baseURL, apiKey, version string
	client                   *http.Client
}

func New(config Config) (*Provider, error) {
	baseURL, err := providers.ValidateBaseURL(config.BaseURL)
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
	return &Provider{baseURL: baseURL, apiKey: apiKey,
		version: version, client: config.HTTPClient}, nil
}

var _ runtimecontract.ModelStream = (*Provider)(nil)

func (p *Provider) Stream(
	ctx context.Context,
	request runtimecontract.ModelRequest,
) (<-chan runtimecontract.ModelStreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("anthropic stream context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	model := strings.TrimSpace(request.Snapshot.Model.ModelID)
	if model == "" {
		return nil, errors.New("anthropic model is required")
	}
	if request.Snapshot.MaxOutputTokens <= 0 {
		return nil, errors.New("anthropic max output tokens is required")
	}
	messages := make([]map[string]any, 0, len(request.Snapshot.Messages))
	for _, message := range request.Snapshot.Messages {
		messages = append(messages, anthropicMessage(message))
	}
	body := map[string]any{"model": model, "messages": messages, "stream": true}
	if reasoning := strings.TrimSpace(request.Snapshot.Model.Reasoning); reasoning != "" && reasoning != "off" {
		body["output_config"] = map[string]string{"effort": reasoning}
	}
	body["max_tokens"] = request.Snapshot.MaxOutputTokens
	if strings.TrimSpace(request.Snapshot.SystemPrompt) != "" {
		body["system"] = request.Snapshot.SystemPrompt
	}
	if len(request.Snapshot.Tools) > 0 {
		tools := make([]map[string]any, 0, len(request.Snapshot.Tools))
		for _, tool := range request.Snapshot.Tools {
			tools = append(tools, map[string]any{
				"name": tool.Name, "description": tool.Description,
				"input_schema": json.RawMessage(tool.InputSchema),
			})
		}
		body["tools"] = tools
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode anthropic request: %w", err)
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
	return streaming.Start(ctx, response, messagesDecoder{}), nil
}

func anthropicMessage(message runtimecontract.TurnMessage) map[string]any {
	content := make([]map[string]any, 0, len(message.Content))
	role := string(message.Role)
	for _, block := range message.Content {
		switch block.Kind {
		case runtimecontract.TurnContentText:
			content = append(content, map[string]any{"type": "text", "text": block.Text})
		case runtimecontract.TurnContentToolUse:
			content = append(content, map[string]any{
				"type": "tool_use", "id": block.ToolCallID, "name": block.ToolName,
				"input": json.RawMessage(block.Input),
			})
		case runtimecontract.TurnContentToolResult:
			role = "user"
			content = append(content, map[string]any{
				"type": "tool_result", "tool_use_id": block.ToolCallID,
				"content": block.Text, "is_error": block.IsError,
			})
		}
	}
	return map[string]any{"role": role, "content": content}
}

type messagesDecoder struct{}

func (messagesDecoder) Decode(
	ctx context.Context,
	reader *streaming.SSEReader,
	emit streaming.EmitFunc,
) (string, error) {
	tools := map[int]*toolAccumulator{}
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		sseEvent, err := reader.Next()
		if err == io.EOF {
			if err := emitTools(emit, tools); err != nil {
				return "", err
			}
			return "", nil
		}
		if err != nil {
			return "", err
		}
		data := strings.TrimSpace(sseEvent.Data)
		var event struct {
			Type  string `json:"type"`
			Index int    `json:"index"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Message struct {
				StopReason string `json:"stop_reason"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return "", fmt.Errorf("decode anthropic stream event: %w", err)
		}
		switch event.Type {
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				tools[event.Index] = &toolAccumulator{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text != "" {
					if err := emit(runtimecontract.ModelStreamEvent{Kind: runtimecontract.StreamTextDelta, Text: event.Delta.Text}); err != nil {
						return "", err
					}
				}
			case "input_json_delta":
				if tool := tools[event.Index]; tool != nil {
					tool.arguments += event.Delta.PartialJSON
				}
			}
		case "message_delta":
			if err := emitTools(emit, tools); err != nil {
				return "", err
			}
			return event.Delta.StopReason, nil
		case "message_stop":
			if err := emitTools(emit, tools); err != nil {
				return "", err
			}
			return "", nil
		case "error":
			return "", errors.New("anthropic stream returned an error")
		}
	}
}

type toolAccumulator struct{ id, name, arguments string }

func emitTool(emit streaming.EmitFunc, tool *toolAccumulator) error {
	if tool == nil || tool.name == "" {
		return nil
	}
	args := json.RawMessage(tool.arguments)
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	return emit(runtimecontract.ModelStreamEvent{
		Kind: runtimecontract.StreamToolCall,
		ToolCall: runtimecontract.ToolCall{
			ID: tool.id, Name: domainsecurity.ToolName(tool.name), Input: args, Arguments: args,
		},
	})
}

func emitTools(emit streaming.EmitFunc, tools map[int]*toolAccumulator) error {
	indices := make([]int, 0, len(tools))
	for index := range tools {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		if err := emitTool(emit, tools[index]); err != nil {
			return err
		}
	}
	return nil
}
