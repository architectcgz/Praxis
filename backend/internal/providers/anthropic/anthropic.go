package anthropic

import (
	"bufio"
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
	runtimecontract "praxis/internal/runtime"
)

type Config struct {
	BaseURL         string
	APIKey          string
	HTTPClient      *http.Client
	Model           string
	Version         string
	MaxOutputTokens int
	Reasoning       string
}

type Provider struct {
	baseURL, apiKey, model, version string
	reasoning                       string
	maxOutputTokens                 int
	client                          *http.Client
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
	reasoning := strings.TrimSpace(config.Reasoning)
	version := strings.TrimSpace(config.Version)
	if version == "" {
		version = "2023-06-01"
	}
	maxOutputTokens := config.MaxOutputTokens
	if maxOutputTokens <= 0 {
		maxOutputTokens = 4096
	}
	return &Provider{baseURL: baseURL, apiKey: apiKey,
		model: strings.TrimSpace(config.Model), version: version, reasoning: reasoning,
		client:          config.HTTPClient,
		maxOutputTokens: maxOutputTokens}, nil
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
	model := p.model
	if model == "" {
		model = request.Snapshot.Model.ModelID
	}
	if model == "" {
		return nil, errors.New("anthropic model is required")
	}
	messages := make([]map[string]any, 0, len(request.Snapshot.Messages))
	for _, message := range request.Snapshot.Messages {
		messages = append(messages, anthropicMessage(message))
	}
	body := map[string]any{"model": model, "messages": messages, "stream": true}
	if p.reasoning != "" && p.reasoning != "off" {
		body["output_config"] = map[string]string{"effort": p.reasoning}
	}
	if p.maxOutputTokens > 0 {
		body["max_tokens"] = p.maxOutputTokens
	}
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
	response, err := providers.RequestClient(p.client).Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, providers.DecodeErrorResponse(response)
	}
	events := make(chan runtimecontract.ModelStreamEvent, 16)
	go func() { defer close(events); defer response.Body.Close(); parse(response.Body, events) }()
	return events, nil
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

func parse(reader io.Reader, events chan<- runtimecontract.ModelStreamEvent) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 2*1024*1024)
	var tools = map[int]*toolAccumulator{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
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
			emitError(events, fmt.Errorf("decode anthropic stream event: %w", err))
			return
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
					events <- runtimecontract.ModelStreamEvent{Kind: runtimecontract.StreamTextDelta, Text: event.Delta.Text}
				}
			case "input_json_delta":
				if tool := tools[event.Index]; tool != nil {
					tool.arguments += event.Delta.PartialJSON
				}
			}
		case "message_delta":
			emitTools(events, tools)
			emitComplete(events, event.Delta.StopReason)
			return
		case "message_stop":
			emitTools(events, tools)
			emitComplete(events, "")
			return
		case "error":
			emitError(events, errors.New("anthropic stream returned an error"))
			return
		}
	}
	if err := scanner.Err(); err != nil {
		emitError(events, err)
	} else {
		emitTools(events, tools)
		emitComplete(events, "")
	}
}

type toolAccumulator struct{ id, name, arguments string }

func emitTool(events chan<- runtimecontract.ModelStreamEvent, tool *toolAccumulator) {
	if tool == nil || tool.name == "" {
		return
	}
	args := json.RawMessage(tool.arguments)
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	events <- runtimecontract.ModelStreamEvent{
		Kind: runtimecontract.StreamToolCall,
		ToolCall: runtimecontract.ToolCall{
			ID: tool.id, Name: domainsecurity.ToolName(tool.name), Input: args, Arguments: args,
		},
	}
}
func emitComplete(events chan<- runtimecontract.ModelStreamEvent, reason string) {
	events <- runtimecontract.ModelStreamEvent{Kind: runtimecontract.StreamComplete, StopReason: reason}
}
func emitError(events chan<- runtimecontract.ModelStreamEvent, err error) {
	events <- runtimecontract.ModelStreamEvent{Kind: runtimecontract.StreamError, Err: err}
}

func emitTools(events chan<- runtimecontract.ModelStreamEvent, tools map[int]*toolAccumulator) {
	indices := make([]int, 0, len(tools))
	for index := range tools {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		emitTool(events, tools[index])
	}
}
