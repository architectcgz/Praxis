package anthropicmessages

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"praxis/internal/provider"
	"praxis/internal/provider/streaming"
)

type Config struct {
	BaseURL       string
	APIKey        string
	Version       string
	ContextWindow int
}

type Dependencies struct {
	HTTPClient *http.Client
}

type Provider struct {
	baseURL, apiKey, version string
	client                   *http.Client
}

// New 使用入口已校验的端点和凭据创建协议适配器，仅补齐默认协议版本。
func New(config Config, deps Dependencies) (*Provider, error) {
	version := config.Version
	if version == "" {
		version = "2023-06-01"
	}
	return &Provider{
		baseURL: config.BaseURL,
		apiKey:  config.APIKey,
		version: version,
		client:  deps.HTTPClient,
	}, nil
}

var _ provider.Provider = (*Provider)(nil)

// Stream 组装完整模型请求并返回统一事件通道；请求无效或连接失败时返回错误。
// 调用方提前停止消费时必须取消 ctx，以释放流资源。
func (p *Provider) Stream(
	ctx context.Context,
	request provider.Request,
) (<-chan provider.Event, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	payload, err := buildPayload(request)
	if err != nil {
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
	response, err := streaming.Open(p.client, httpRequest)
	if err != nil {
		return nil, fmt.Errorf("anthropic request: %w", streaming.RedactError(err, sensitiveStateValues(request.Context)))
	}
	return streaming.Start(ctx, response, decoder{}, sensitiveStateValues(request.Context)...), nil
}

func buildPayload(request provider.Request) ([]byte, error) {
	modelID := request.Model.ModelID
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
	// 推理等级已在模型加载时校验并固定，此处只映射协议字段。
	if reasoning := request.Model.ReasoningLevel; reasoning != "" {
		if reasoning == "off" {
			body.Thinking = &anthropicThinking{Type: "disabled"}
		} else {
			body.OutputConfig = &anthropicOutputConfig{Effort: reasoning}
		}
	}
	if schema := request.OutputSchema; schema != nil {
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
