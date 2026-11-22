package model

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
)

const MaxReasoningStateBytes = 1024 * 1024

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
// 此类型只承载数据；协议解析和来源绑定由对应边界负责。
type ReasoningState struct {
	ProviderID string
	Protocol   string
	ModelID    string
	Payload    json.RawMessage
}

// Clone 深拷贝载荷；nil 状态仍返回 nil，避免持久化与观察者共享可变字节。
func (s *ReasoningState) Clone() *ReasoningState {
	if s == nil {
		return nil
	}
	copied := *s
	copied.Payload = bytes.Clone(s.Payload)
	return &copied
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
// 只接收输入边界已校验的配置，加载时准备各个已配置推理等级的完整选项。
// 模型配置查询直接返回已准备的只读配置，不重新校验或覆盖参数。
type Catalog struct {
	options []Option
	models  map[modelKey]Config
}

type modelKey struct {
	ProviderID     string
	ModelID        string
	ReasoningLevel string
}

func NewCatalog(document Document) *Catalog {
	catalog := &Catalog{
		options: make([]Option, 0),
		models:  make(map[modelKey]Config),
	}
	for _, provider := range document.Providers {
		for _, candidate := range provider.Models {
			prepared := Config{
				ProviderID:      provider.ID,
				ModelID:         candidate.ID,
				ReasoningLevel:  candidate.DefaultReasoningLevel,
				ReasoningLevels: slices.Clone(candidate.ReasoningLevels),
				Thinking:        candidate.Thinking,
				APIFormat:       string(candidate.APIFormat),
				ContextWindow:   candidate.ContextWindow,
				MaxOutputTokens: candidate.MaxOutputTokens,
				BaseURL:         provider.BaseURL,
				ProxyURL:        provider.ProxyURL,
			}
			// 空等级定位默认选项，其他等级在加载时展开，查询不再覆盖默认值。
			key := modelKey{ProviderID: provider.ID, ModelID: candidate.ID}
			catalog.models[key] = prepared
			for _, level := range candidate.ReasoningLevels {
				key.ReasoningLevel = level
				prepared.ReasoningLevel = level
				catalog.models[key] = prepared
			}
			catalog.options = append(catalog.options, Option{
				GroupID:               candidate.GroupID,
				ProviderID:            provider.ID,
				ModelID:               candidate.ID,
				Label:                 candidate.DisplayName,
				ProviderName:          provider.DisplayName,
				ReasoningLevels:       slices.Clone(candidate.ReasoningLevels),
				DefaultReasoningLevel: candidate.DefaultReasoningLevel,
			})
		}
	}
	orderModelOptions(catalog.options, document.Groups)
	return catalog
}

func (c *Catalog) ListModels() []Option {
	options := slices.Clone(c.options)
	for index := range options {
		options[index].ReasoningLevels = slices.Clone(options[index].ReasoningLevels)
	}
	return options
}

func (c *Catalog) Get(providerID, modelID, reasoningLevel string) (Config, error) {
	config, ok := c.models[modelKey{
		ProviderID:     providerID,
		ModelID:        modelID,
		ReasoningLevel: reasoningLevel,
	}]
	if !ok {
		return Config{}, fmt.Errorf("model %q for provider %q with reasoning level %q is not configured: %w", modelID, providerID, reasoningLevel, ErrNotConfigured)
	}
	return config, nil
}

func orderModelOptions(options []Option, groups []Group) {
	groupOrder := make(map[string]int, len(groups))
	for index, group := range groups {
		groupOrder[group.ID] = index
	}
	slices.SortStableFunc(options, func(left, right Option) int {
		leftGroup, leftOK := groupOrder[left.GroupID]
		rightGroup, rightOK := groupOrder[right.GroupID]
		if leftOK && rightOK && leftGroup != rightGroup {
			return leftGroup - rightGroup
		}
		if leftOK != rightOK {
			if leftOK {
				return -1
			}
			return 1
		}
		if left.Label == right.Label {
			if left.ProviderID == right.ProviderID {
				return strings.Compare(left.ModelID, right.ModelID)
			}
			return strings.Compare(left.ProviderID, right.ProviderID)
		}
		return strings.Compare(left.Label, right.Label)
	})
}
