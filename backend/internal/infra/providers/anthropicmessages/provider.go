package anthropicmessages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"

	appcontext "praxis/internal/core/context"
	"praxis/internal/core/model"
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

// New 使用入口已校验的端点创建协议适配器，检查凭据并补齐默认协议版本。
func New(config Config) (*Provider, error) {
	apiKey := strings.TrimSpace(config.APIKey)
	if apiKey == "" {
		return nil, errors.New("anthropic API key is required")
	}
	version := strings.TrimSpace(config.Version)
	if version == "" {
		version = "2023-06-01"
	}
	return &Provider{
		baseURL:       config.BaseURL,
		apiKey:        apiKey,
		version:       version,
		client:        config.HTTPClient,
		contextWindow: config.ContextWindow,
	}, nil
}

var _ model.ModelStream = (*Provider)(nil)

// Stream 组装完整模型请求并返回统一事件通道；请求无效或连接失败时返回错误。
// 调用方提前停止消费时必须取消 ctx，以释放流资源。
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
	payload, err := buildPayload(request)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("anthropic request: %w", streaming.RedactError(err, sensitiveStateValues(request.Context)))
	}
	return streaming.Start(ctx, response, decoder{}, sensitiveStateValues(request.Context)...), nil
}

func buildPayload(request model.ModelRequest) ([]byte, error) {
	modelID := request.Model.ModelID
	if modelID == "" {
		return nil, errors.New("anthropic model is required")
	}
	if request.MaxOutputTokens <= 0 {
		return nil, errors.New("anthropic max output tokens is required")
	}
	encodedContext, err := encodeContext(request.Context, request.Model)
	if err != nil {
		return nil, err
	}
	// 摘要等请求会缩小输出上限，需检查本次请求是否仍能容纳冻结的 thinking 预算。
	if request.Model.Thinking.Mode == "enabled" && request.Model.Thinking.BudgetTokens >= request.MaxOutputTokens {
		return nil, errors.New("thinking budget must be less than request max output tokens")
	}
	body := anthropicRequest{
		Model:     modelID,
		Messages:  encodedContext.Messages,
		System:    encodedContext.System,
		Stream:    true,
		MaxTokens: request.MaxOutputTokens,
		// 自动缓存随对话增长推进断点，复用工具定义和 system 前缀。
		CacheControl: anthropicCacheControl{Type: "ephemeral"},
	}
	if request.Model.Thinking.Mode != "" {
		body.Thinking = &anthropicThinking{Type: request.Model.Thinking.Mode, BudgetTokens: request.Model.Thinking.BudgetTokens}
	}
	if reasoning := request.Model.ReasoningLevel; reasoning != "" {
		if !slices.Contains(request.Model.ReasoningLevels, reasoning) {
			return nil, errors.New("anthropic effort is not supported by frozen model capabilities")
		}
		if reasoning == "off" {
			if !request.Model.Thinking.SupportsDisabled {
				return nil, errors.New("anthropic model does not support disabled thinking")
			}
			body.Thinking = &anthropicThinking{Type: "disabled"}
		} else {
			if !slices.Contains([]string{"low", "medium", "high", "xhigh", "max"}, reasoning) {
				return nil, errors.New("unsupported anthropic effort")
			}
			body.OutputConfig = &anthropicOutputConfig{Effort: reasoning}
		}
	}
	if schema := request.OutputSchema; schema != nil {
		if err := schema.Validate(); err != nil {
			return nil, fmt.Errorf("anthropic output schema: %w", err)
		}
		// 格式与 effort 共用 output_config，追加格式时保留已选的推理强度。
		if body.OutputConfig == nil {
			body.OutputConfig = &anthropicOutputConfig{}
		}
		body.OutputConfig.Format = &anthropicJSONSchema{
			Type:   "json_schema",
			Schema: schema.Schema,
		}
	}
	for _, tool := range request.Tools {
		body.Tools = append(body.Tools, anthropicToolDefinition{
			Name:        string(tool.Name),
			Description: tool.Description,
			InputSchema: json.RawMessage(tool.InputSchema),
		})
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("encode anthropic request: %w", err)
	}
	return payload, nil
}
