package openaicompat

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

	"praxis/internal/core/domain"
	coreruntime "praxis/internal/core/runtime"
	"praxis/internal/providers"
)

type Protocol string

const (
	ProtocolChatCompletions Protocol = "chat_completions"
	ProtocolResponses       Protocol = "responses"
)

type Config struct {
	BaseURL         string
	APIKey          string
	HTTPClient      *http.Client
	Protocol        Protocol
	Model           string
	MaxOutputTokens int
	Reasoning       string
}

type Provider struct {
	baseURL         string
	apiKey          string
	client          *http.Client
	protocol        Protocol
	model           string
	maxOutputTokens int
	reasoning       string
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
	if protocol != ProtocolChatCompletions && protocol != ProtocolResponses {
		return nil, fmt.Errorf("unsupported openai-compatible protocol %q", protocol)
	}
	reasoning := strings.TrimSpace(config.Reasoning)
	return &Provider{
		baseURL: baseURL, apiKey: apiKey,
		client: config.HTTPClient, protocol: protocol, model: strings.TrimSpace(config.Model),
		maxOutputTokens: config.MaxOutputTokens, reasoning: reasoning,
	}, nil
}

var _ coreruntime.ModelStream = (*Provider)(nil)

func (p *Provider) Stream(
	ctx context.Context,
	request coreruntime.ModelRequest,
) (<-chan coreruntime.ModelStreamEvent, error) {
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
	response, err := providers.RequestClient(p.client).Do(httpRequest)
	if err != nil {
		return nil, fmt.Errorf("openai-compatible request: %w", err)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, providers.DecodeErrorResponse(response)
	}
	events := make(chan coreruntime.ModelStreamEvent, 16)
	go func() {
		defer close(events)
		defer response.Body.Close()
		if p.protocol == ProtocolResponses {
			parseResponses(response.Body, events)
			return
		}
		parseChat(response.Body, events)
	}()
	return events, nil
}

func (p *Provider) requestPayload(snapshot coreruntime.TurnSnapshot) ([]byte, string, error) {
	model := p.model
	if model == "" {
		model = snapshot.Model.ModelID
	}
	if strings.TrimSpace(model) == "" {
		return nil, "", errors.New("openai-compatible model is required")
	}
	if p.protocol == ProtocolResponses {
		input := make([]map[string]any, 0, len(snapshot.Messages)+1)
		if strings.TrimSpace(snapshot.SystemPrompt) != "" {
			input = append(input, map[string]any{"role": "system", "content": snapshot.SystemPrompt})
		}
		for _, message := range snapshot.Messages {
			input = append(input, responseMessage(message))
		}
		body := map[string]any{"model": model, "input": input, "stream": true}
		if p.reasoning != "" && p.reasoning != "off" {
			body["reasoning"] = map[string]string{"effort": p.reasoning}
		}
		if p.maxOutputTokens > 0 {
			body["max_output_tokens"] = p.maxOutputTokens
		}
		if len(snapshot.Tools) > 0 {
			tools := make([]map[string]any, 0, len(snapshot.Tools))
			for _, tool := range snapshot.Tools {
				tools = append(tools, map[string]any{
					"type":        "function",
					"name":        tool.Name,
					"description": tool.Description,
					"parameters":  json.RawMessage(tool.InputSchema),
				})
			}
			body["tools"] = tools
		}
		encoded, err := json.Marshal(body)
		return encoded, "/responses", err
	}
	messages := make([]map[string]any, 0, len(snapshot.Messages)+1)
	if strings.TrimSpace(snapshot.SystemPrompt) != "" {
		messages = append(messages, map[string]any{"role": "system", "content": snapshot.SystemPrompt})
	}
	for _, message := range snapshot.Messages {
		messages = append(messages, chatMessage(message))
	}
	body := map[string]any{"model": model, "messages": messages, "stream": true}
	if p.reasoning != "" && p.reasoning != "off" {
		body["reasoning_effort"] = p.reasoning
	}
	if p.maxOutputTokens > 0 {
		body["max_tokens"] = p.maxOutputTokens
	}
	if len(snapshot.Tools) > 0 {
		tools := make([]map[string]any, 0, len(snapshot.Tools))
		for _, tool := range snapshot.Tools {
			tools = append(tools, map[string]any{"type": "function", "function": map[string]any{
				"name": tool.Name, "description": tool.Description, "parameters": json.RawMessage(tool.InputSchema),
			}})
		}
		body["tools"] = tools
	}
	encoded, err := json.Marshal(body)
	return encoded, "/chat/completions", err
}

func chatMessage(message coreruntime.TurnMessage) map[string]any {
	result := map[string]any{"role": string(message.Role)}
	var text strings.Builder
	toolCalls := make([]map[string]any, 0)
	for _, block := range message.Content {
		switch block.Kind {
		case coreruntime.TurnContentText:
			text.WriteString(block.Text)
		case coreruntime.TurnContentToolUse:
			toolCalls = append(toolCalls, map[string]any{
				"id":   block.ToolCallID,
				"type": "function",
				"function": map[string]string{
					"name": block.ToolName, "arguments": string(block.Input),
				},
			})
		case coreruntime.TurnContentToolResult:
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

func responseMessage(message coreruntime.TurnMessage) map[string]any {
	return chatMessage(message)
}

func parseChat(reader io.Reader, events chan<- coreruntime.ModelStreamEvent) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 2*1024*1024)
	var calls = map[int]*toolAccumulator{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "[DONE]" {
			emitChatTools(events, calls)
			emitComplete(events, "")
			return
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content   string `json:"content"`
					ToolCalls []struct {
						Index    int    `json:"index"`
						ID       string `json:"id"`
						Function struct {
							Name      string `json:"name"`
							Arguments string `json:"arguments"`
						} `json:"function"`
					} `json:"tool_calls"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(data), &chunk); err != nil {
			emitError(events, fmt.Errorf("decode openai-compatible stream event: %w", err))
			return
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.Delta.Content != "" {
			events <- coreruntime.ModelStreamEvent{Kind: coreruntime.StreamTextDelta, Text: choice.Delta.Content}
		}
		for _, delta := range choice.Delta.ToolCalls {
			call := calls[delta.Index]
			if call == nil {
				call = &toolAccumulator{}
				calls[delta.Index] = call
			}
			if delta.ID != "" {
				call.id = delta.ID
			}
			if delta.Function.Name != "" {
				call.name = delta.Function.Name
			}
			call.arguments += delta.Function.Arguments
		}
		if choice.FinishReason != "" {
			emitChatTools(events, calls)
			emitComplete(events, choice.FinishReason)
			return
		}
	}
	if err := scanner.Err(); err != nil {
		emitError(events, err)
	} else {
		emitComplete(events, "")
	}
}

type toolAccumulator struct{ id, name, arguments string }

func emitTool(events chan<- coreruntime.ModelStreamEvent, call *toolAccumulator) {
	if call == nil || call.name == "" {
		return
	}
	args := json.RawMessage(call.arguments)
	if len(bytes.TrimSpace(args)) == 0 {
		args = json.RawMessage(`{}`)
	}
	events <- coreruntime.ModelStreamEvent{
		Kind: coreruntime.StreamToolCall,
		ToolCall: coreruntime.ToolCall{
			ID:        call.id,
			Name:      domain.ToolName(call.name),
			Input:     append(json.RawMessage(nil), args...),
			Arguments: append(json.RawMessage(nil), args...),
		},
	}
}

func emitChatTools(events chan<- coreruntime.ModelStreamEvent, calls map[int]*toolAccumulator) {
	indices := make([]int, 0, len(calls))
	for index := range calls {
		indices = append(indices, index)
	}
	sort.Ints(indices)
	for _, index := range indices {
		emitTool(events, calls[index])
	}
}

func parseResponses(reader io.Reader, events chan<- coreruntime.ModelStreamEvent) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 1024), 2*1024*1024)
	calls := map[string]*toolAccumulator{}
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}
		if strings.HasPrefix(line, "event:") {
			continue
		}
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		var event struct {
			Type  string `json:"type"`
			Delta string `json:"delta"`
			Item  struct {
				ID        string `json:"id"`
				CallID    string `json:"call_id"`
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
				Type      string `json:"type"`
			} `json:"item"`
			CallID    string `json:"call_id"`
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
			Response  struct {
				Status string `json:"status"`
			} `json:"response"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			emitError(events, fmt.Errorf("decode openai responses stream event: %w", err))
			return
		}
		switch event.Type {
		case "response.output_text.delta":
			if event.Delta != "" {
				events <- coreruntime.ModelStreamEvent{Kind: coreruntime.StreamTextDelta, Text: event.Delta}
			}
		case "response.output_item.added":
			if event.Item.Type == "function_call" {
				callID := event.Item.CallID
				if callID == "" {
					callID = event.Item.ID
				}
				calls[callID] = &toolAccumulator{id: callID, name: event.Item.Name}
			}
		case "response.output_item.done":
			if event.Item.Type == "function_call" {
				callID := event.Item.CallID
				if callID == "" {
					callID = event.Item.ID
				}
				call := calls[callID]
				if call == nil {
					call = &toolAccumulator{id: callID, name: event.Item.Name}
					calls[callID] = call
				}
				if event.Item.Arguments != "" {
					call.arguments = event.Item.Arguments
				}
			}
		case "response.function_call_arguments.delta":
			call := calls[event.CallID]
			if call == nil {
				call = &toolAccumulator{id: event.CallID, name: event.Name}
				calls[event.CallID] = call
			}
			call.arguments += event.Delta
		case "response.function_call_arguments.done":
			if call := calls[event.CallID]; call != nil && event.Arguments != "" {
				call.arguments = event.Arguments
			}
		case "response.completed", "response.done":
			emitResponseTools(events, calls)
			emitComplete(events, event.Response.Status)
			return
		case "error":
			emitError(events, errors.New("openai responses stream returned an error"))
			return
		}
	}
	if err := scanner.Err(); err != nil {
		emitError(events, err)
	} else {
		emitResponseTools(events, calls)
		emitComplete(events, "")
	}
}

func emitComplete(events chan<- coreruntime.ModelStreamEvent, reason string) {
	events <- coreruntime.ModelStreamEvent{Kind: coreruntime.StreamComplete, StopReason: reason}
}

func emitResponseTools(events chan<- coreruntime.ModelStreamEvent, calls map[string]*toolAccumulator) {
	keys := make([]string, 0, len(calls))
	for key := range calls {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		emitTool(events, calls[key])
	}
}
func emitError(events chan<- coreruntime.ModelStreamEvent, err error) {
	events <- coreruntime.ModelStreamEvent{Kind: coreruntime.StreamError, Err: err}
}
