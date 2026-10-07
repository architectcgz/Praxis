package anthropicmessages

import (
	"praxis/internal/contracts"

	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"sort"
	"strings"

	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
	modelconfig "praxis/internal/core/model/config"
	"praxis/internal/infra/providers"
	"praxis/internal/infra/providers/streaming"
)

type Config struct {
	BaseURL       string
	APIKey        string
	HTTPClient    *http.Client
	Version       string
	ContextWindow int
}

type Provider struct {
	baseURL, apiKey, version string
	client                   *http.Client
	contextWindow            int
}

func New(config Config) (*Provider, error) {
	baseURL, err := modelconfig.ValidateBaseURL(config.BaseURL)
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
	return &Provider{
		baseURL:       baseURL,
		apiKey:        apiKey,
		version:       version,
		client:        config.HTTPClient,
		contextWindow: config.ContextWindow,
	}, nil
}

var _ model.ModelStream = (*Provider)(nil)

func (p *Provider) Stream(
	ctx context.Context,
	request model.ModelRequest,
) (<-chan model.ModelStreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("anthropic stream context is required")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	modelID := request.Model.ModelID
	if modelID == "" {
		return nil, errors.New("anthropic model is required")
	}
	if request.MaxOutputTokens <= 0 {
		return nil, errors.New("anthropic max output tokens is required")
	}
	messages := make([]map[string]any, 0, len(request.Context.Entries))
	for _, entry := range request.Context.Entries {
		messages = append(messages, anthropicContextEntry(entry))
	}
	body := map[string]any{
		"model":    modelID,
		"messages": messages,
		"stream":   true,
		// 自动缓存随对话增长推进断点，复用包含工具定义和 system 的相同前缀。
		"cache_control": map[string]string{"type": "ephemeral"},
	}
	if reasoning := request.Model.ReasoningLevel; reasoning != "" && reasoning != "off" {
		body["output_config"] = map[string]string{"effort": reasoning}
	}
	body["max_tokens"] = request.MaxOutputTokens
	if request.Context.SystemPrompt != "" {
		body["system"] = request.Context.SystemPrompt
	}
	if len(request.Tools) > 0 {
		tools := make([]map[string]any, 0, len(request.Tools))
		for _, tool := range request.Tools {
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
	if err := appcontext.ExceedsContextWindow(payload, request.MaxOutputTokens, p.contextWindow); err != nil {
		return nil, err
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

func anthropicContextEntry(entry appcontext.ContextEntry) map[string]any {
	content := make([]map[string]any, 0, len(entry.Content))
	role := string(entry.Role)
	for _, block := range entry.Content {
		switch block.Kind {
		case appcontext.ContextBlockText:
			content = append(content, map[string]any{"type": "text", "text": block.Text})
		case appcontext.ContextBlockToolCall:
			content = append(content, map[string]any{
				"type": "tool_use", "id": block.CallID, "name": block.Name,
				"input": json.RawMessage(block.Input),
			})
		case appcontext.ContextBlockToolResult:
			role = "user"
			content = append(content, map[string]any{
				"type": "tool_result", "tool_use_id": block.CallID,
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
	var usage *model.ModelUsage
	var stopReason string
	for {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		sseEvent, err := reader.Next()
		if err == io.EOF {
			if err := emitTools(emit, tools); err != nil {
				return "", err
			}
			return stopReason, nil
		}
		if err != nil {
			return "", err
		}
		data := strings.TrimSpace(sseEvent.Data)
		var event struct {
			Type  string        `json:"type"`
			Index int           `json:"index"`
			Usage *messageUsage `json:"usage"`
			Delta struct {
				Type        string `json:"type"`
				Text        string `json:"text"`
				Thinking    string `json:"thinking"`
				PartialJSON string `json:"partial_json"`
				StopReason  string `json:"stop_reason"`
			} `json:"delta"`
			ContentBlock struct {
				Type string `json:"type"`
				ID   string `json:"id"`
				Name string `json:"name"`
			} `json:"content_block"`
			Message struct {
				Usage *messageUsage `json:"usage"`
			} `json:"message"`
		}
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return "", fmt.Errorf("decode anthropic stream event: %w", err)
		}
		switch event.Type {
		case "message_start":
			usage = normalizeMessageUsage(event.Message.Usage)
			if usage != nil {
				if err := emit(model.ModelStreamEvent{Kind: model.StreamUsage, Usage: usage}); err != nil {
					return "", err
				}
			}
		case "content_block_start":
			if event.ContentBlock.Type == "tool_use" {
				tools[event.Index] = &toolAccumulator{id: event.ContentBlock.ID, name: event.ContentBlock.Name}
			}
		case "content_block_delta":
			switch event.Delta.Type {
			case "text_delta":
				if event.Delta.Text != "" {
					if err := emit(model.ModelStreamEvent{Kind: model.StreamTextDelta, Text: event.Delta.Text}); err != nil {
						return "", err
					}
				}
			case "thinking_delta":
				if event.Delta.Thinking != "" {
					if err := emit(model.ModelStreamEvent{Kind: model.StreamThinkingDelta, Text: event.Delta.Thinking}); err != nil {
						return "", err
					}
				}
			case "input_json_delta":
				if tool := tools[event.Index]; tool != nil {
					tool.arguments += event.Delta.PartialJSON
				}
			}
		case "message_delta":
			// message_delta 的输出计数是累计值，保留起始输入计数并替换输出。
			if usage != nil && event.Usage != nil && event.Usage.OutputTokens != nil {
				next := *usage
				next.OutputTokens = event.Usage.OutputTokens
				if next.Valid() && (usage.OutputTokens == nil || *next.OutputTokens >= *usage.OutputTokens) {
					usage = &next
					if err := emit(model.ModelStreamEvent{Kind: model.StreamUsage, Usage: usage}); err != nil {
						return "", err
					}
				}
			}
			if event.Delta.StopReason != "" {
				stopReason = event.Delta.StopReason
			}
		case "message_stop":
			if err := emitTools(emit, tools); err != nil {
				return "", err
			}
			return stopReason, nil
		case "error":
			return "", errors.New("anthropic stream returned an error")
		}
	}
}

type messageUsage struct {
	InputTokens              *int64 `json:"input_tokens"`
	OutputTokens             *int64 `json:"output_tokens"`
	CacheReadInputTokens     *int64 `json:"cache_read_input_tokens"`
	CacheCreationInputTokens *int64 `json:"cache_creation_input_tokens"`
}

func normalizeMessageUsage(reported *messageUsage) *model.ModelUsage {
	if reported == nil || reported.InputTokens == nil || *reported.InputTokens < 0 {
		return nil
	}
	// Anthropic 的 input_tokens 不含缓存读取和写入，统一分母时需补上两者。
	total := *reported.InputTokens
	for _, count := range []*int64{reported.CacheReadInputTokens, reported.CacheCreationInputTokens} {
		if count != nil {
			if *count < 0 || *count > math.MaxInt64-total {
				return nil
			}
			total += *count
		}
	}
	usage := &model.ModelUsage{
		InputTokens:              total,
		OutputTokens:             reported.OutputTokens,
		CacheReadInputTokens:     reported.CacheReadInputTokens,
		CacheCreationInputTokens: reported.CacheCreationInputTokens,
	}
	if !usage.Valid() {
		return nil
	}
	return usage
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
	return emit(model.ModelStreamEvent{
		Kind: model.StreamToolCall,
		ToolCall: contracts.ToolCall{
			ID: tool.id, Name: contracts.ToolName(tool.name), Arguments: args,
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
