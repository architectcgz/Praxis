package model

import (
	"encoding/json"
	"fmt"
	"praxis/internal/errorcode"
)

const (
	ErrNotConfigured       errorcode.Code = "model.not_configured"
	MaxReasoningStateBytes                = 1024 * 1024
)

// APIFormat 标识 Provider 使用的模型 API 协议。
type APIFormat string

const (
	APIFormatAnthropicMessages     APIFormat = "anthropic_messages"
	APIFormatOpenAIResponses       APIFormat = "openai_responses"
	APIFormatOpenAIChatCompletions APIFormat = "openai_chat_completions"
)

// Valid 判断模型 API 协议是否受支持。
func (f APIFormat) Valid() bool {
	switch f {
	case APIFormatAnthropicMessages, APIFormatOpenAIResponses, APIFormatOpenAIChatCompletions:
		return true
	default:
		return false
	}
}

// ModelThinking 冻结模型的 Thinking 配置与禁用能力；Effort 由独立的 ReasoningLevels 表达。
// Mode 为空表示使用服务端默认模式，不据此推断是否会产生 thinking blocks。
type ModelThinking struct {
	Mode             string `json:"mode,omitempty"`
	BudgetTokens     int    `json:"budgetTokens,omitzero"`
	SupportsDisabled bool   `json:"supportsDisabled,omitzero"`
}

// Option 是模型目录中的可选条目，携带分组与推理能力。
type Option struct {
	GroupID               string
	ProviderID            string
	ModelID               string
	Label                 string
	ProviderName          string
	ReasoningLevels       []string
	DefaultReasoningLevel string
}

// CatalogEntry 保存一条已在输入边界校验并排序的模型目录项及默认运行时配置。
type CatalogEntry struct {
	Option Option
	Config Config
}

// Config 保存 Run 选定并持久化的只读模型配置，能力列表在执行链路中共享。
//
// 这些字段不能在 Run 运行期间从当前模型配置重新读取，否则修改配置
// 会改变已经创建的 Run 的请求协议和资源边界。
type Config struct {
	ProviderID      string
	ModelID         string
	ReasoningLevel  string
	ReasoningLevels []string      `json:",omitempty"`
	Thinking        ModelThinking `json:",omitzero"`
	APIFormat       string
	ContextWindow   int
	MaxOutputTokens int
	BaseURL         string
	ProxyURL        string
}

// CanReplayReasoningState 判断完整状态的来源是否与目标 Provider、API 和模型完全一致；nil 状态不可重放。
func (s Config) CanReplayReasoningState(state *ReasoningState) bool {
	return state != nil && state.ProviderID == s.ProviderID && state.Protocol == s.APIFormat && state.ModelID == s.ModelID
}

// ReasoningState 保存模型生成的推理状态，仅供相同 Provider、协议与模型原样续传；不用于展示或转换为文本。
// 此类型只承载数据；协议解析和来源绑定由对应边界负责，发布后只读。
type ReasoningState struct {
	ProviderID string
	Protocol   string
	ModelID    string
	Payload    json.RawMessage
}

// Usage 记录一次模型请求的已知用量；输入计数包含已知的缓存读取和写入。
type Usage struct {
	InputTokens              *int64 `json:"inputTokens,omitempty"`
	OutputTokens             *int64 `json:"outputTokens,omitempty"`
	CacheReadInputTokens     *int64 `json:"cacheReadInputTokens,omitempty"`
	CacheCreationInputTokens *int64 `json:"cacheCreationInputTokens,omitempty"`
}

func (u Usage) Reported() bool {
	return u.InputTokens != nil || u.OutputTokens != nil || u.CacheReadInputTokens != nil || u.CacheCreationInputTokens != nil
}

// UsageTotals 累计模型请求的已知计数；报告数用于区分未知分项与已知零值。
type UsageTotals struct {
	InputTokens              int64  `json:"inputTokens"`
	OutputTokens             int64  `json:"outputTokens"`
	CacheReadInputTokens     int64  `json:"cacheReadInputTokens"`
	CacheCreationInputTokens int64  `json:"cacheCreationInputTokens"`
	ReportedRequests         uint64 `json:"reportedRequests"`
	InputReports             uint64 `json:"inputReports"`
	OutputReports            uint64 `json:"outputReports"`
	CacheReadReports         uint64 `json:"cacheReadReports"`
	CacheCreationReports     uint64 `json:"cacheCreationReports"`
}

func (u *UsageTotals) Add(value Usage) {
	if !value.Reported() {
		return
	}
	u.ReportedRequests++
	if value.InputTokens != nil {
		u.InputTokens += *value.InputTokens
		u.InputReports++
	}
	if value.OutputTokens != nil {
		u.OutputTokens += *value.OutputTokens
		u.OutputReports++
	}
	if value.CacheReadInputTokens != nil {
		u.CacheReadInputTokens += *value.CacheReadInputTokens
		u.CacheReadReports++
	}
	if value.CacheCreationInputTokens != nil {
		u.CacheCreationInputTokens += *value.CacheCreationInputTokens
		u.CacheCreationReports++
	}
}

// Catalog 保存加载后的只读模型目录，不包含凭据或存储能力。
// 只接收输入边界已校验并排序的条目，加载时展开各个已配置推理等级的完整配置。
// 模型配置查询直接返回已准备的只读配置，不重新校验或覆盖参数。
type Catalog struct {
	options      []Option
	models       map[modelKey]Config
	defaultModel Option
}

type modelKey struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

func NewCatalog(entries []CatalogEntry, defaultModel Option) *Catalog {
	catalog := &Catalog{
		options:      make([]Option, 0, len(entries)),
		models:       make(map[modelKey]Config, len(entries)),
		defaultModel: defaultModel,
	}
	for _, entry := range entries {
		prepared := entry.Config
		// 空等级定位默认选项，其他等级在加载时展开，查询不再覆盖默认值。
		key := modelKey{
			ProviderID: prepared.ProviderID,
			ModelID:    prepared.ModelID,
		}
		catalog.models[key] = prepared
		for _, level := range prepared.ReasoningLevels {
			key.ReasoningLevel = level
			prepared.ReasoningLevel = level
			catalog.models[key] = prepared
		}
		catalog.options = append(catalog.options, entry.Option)
	}
	return catalog
}

// ListModels 返回只读模型选项列表，调用方不得修改切片或其推理等级列表。
func (c *Catalog) ListModels() []Option {
	return c.options
}

// DefaultModel 返回会话可继承的模型；空目录返回零值。
func (c *Catalog) DefaultModel() Option {
	return c.defaultModel
}

func (c *Catalog) Get(providerID, modelID, reasoningLevel string) (Config, error) {
	config, ok := c.models[modelKey{
		ProviderID:     providerID,
		ModelID:        modelID,
		ReasoningLevel: reasoningLevel,
	}]
	if !ok {
		return Config{}, fmt.Errorf(
			"model %q for provider %q with reasoning level %q is not configured: %w",
			modelID,
			providerID,
			reasoningLevel,
			ErrNotConfigured,
		)
	}
	return config, nil
}
