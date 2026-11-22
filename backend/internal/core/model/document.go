package model

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"praxis/internal/errorcode"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

const ErrNotConfigured errorcode.Code = "model.not_configured"

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ModelThinking 冻结模型的 Thinking 配置与禁用能力；Effort 由独立的 ReasoningLevels 表达。
// Mode 为空表示使用服务端默认模式，不据此推断是否会产生 thinking blocks。
type ModelThinking struct {
	Mode             string `json:"mode,omitempty"`
	BudgetTokens     int    `json:"budgetTokens,omitzero"`
	SupportsDisabled bool   `json:"supportsDisabled,omitzero"`
}

// APIFormat 标识 Provider 使用的模型 API 协议。
type APIFormat string

const (
	APIFormatAnthropicMessages     APIFormat = "anthropic_messages"
	APIFormatOpenAIResponses       APIFormat = "openai_responses"
	APIFormatOpenAIChatCompletions APIFormat = "openai_chat_completions"
)

// Group 定义模型目录中的分组及展示名称。
type Group struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// Model 保存可编辑的模型能力与推理等级，不包含运行时凭据。
type Model struct {
	ID                    string        `json:"id"`
	DisplayName           string        `json:"displayName"`
	GroupID               string        `json:"groupId"`
	APIFormat             APIFormat     `json:"apiFormat"`
	ContextWindow         int           `json:"contextWindow"`
	MaxOutputTokens       int           `json:"maxOutputTokens"`
	ReasoningLevels       []string      `json:"reasoningLevels,omitempty"`
	DefaultReasoningLevel string        `json:"defaultReasoningLevel,omitempty"`
	Thinking              ModelThinking `json:"thinking,omitzero"`
}

// Provider 保存模型端点和模型目录；密钥由注册表独立管理。
type Provider struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"displayName"`
	BaseURL     string  `json:"baseUrl"`
	ProxyURL    string  `json:"proxyUrl,omitempty"`
	Models      []Model `json:"models"`
}

// Document 是模型配置的完整文档；业务层只使用输入边界已规范化的值。
type Document struct {
	Groups    []Group    `json:"groups"`
	Providers []Provider `json:"providers"`
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

// Clone 返回配置文档的深拷贝，调用方修改嵌套模型或推理等级不会影响原配置。
func (d Document) Clone() Document {
	d.Groups = slices.Clone(d.Groups)
	d.Providers = slices.Clone(d.Providers)
	for index := range d.Providers {
		d.Providers[index].Models = slices.Clone(d.Providers[index].Models)
		for modelIndex := range d.Providers[index].Models {
			configured := &d.Providers[index].Models[modelIndex]
			configured.ReasoningLevels = slices.Clone(configured.ReasoningLevels)
		}
	}
	return d
}

// ValidateDocument 在请求或配置文件输入边界只读校验完整模型配置文档，不清洗字段或补默认值。
func ValidateDocument(document Document) error {
	groupsSeen, err := validateGroups(document.Groups)
	if err != nil {
		return err
	}
	return validateProviders(document.Providers, groupsSeen)
}

func validateGroups(groups []Group) (map[string]struct{}, error) {
	seen := make(map[string]struct{}, len(groups))
	for _, group := range groups {
		if !idPattern.MatchString(group.ID) {
			return nil, fmt.Errorf("models config: invalid group id %q", group.ID)
		}
		if group.DisplayName == "" || !canonicalString(group.DisplayName) {
			return nil, fmt.Errorf("models config: group %q display name is required", group.ID)
		}
		if _, exists := seen[group.ID]; exists {
			return nil, fmt.Errorf("models config: duplicate group id %q", group.ID)
		}
		seen[group.ID] = struct{}{}
	}
	return seen, nil
}

func validateProviders(providers []Provider, groupsSeen map[string]struct{}) error {
	seen := make(map[string]struct{}, len(providers))
	for _, provider := range providers {
		if !idPattern.MatchString(provider.ID) {
			return fmt.Errorf("models config: invalid provider id %q", provider.ID)
		}
		if _, exists := seen[provider.ID]; exists {
			return fmt.Errorf("models config: duplicate provider id %q", provider.ID)
		}
		seen[provider.ID] = struct{}{}
		if provider.DisplayName == "" || !canonicalString(provider.DisplayName) {
			return fmt.Errorf("models config: provider %q display name must be normalized and non-empty", provider.ID)
		}
		if err := validateBaseURL(provider.BaseURL); err != nil {
			return fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		if err := validateProxyURL(provider.ProxyURL); err != nil {
			return fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		if err := validateModels(provider, groupsSeen); err != nil {
			return err
		}
	}
	return nil
}

func validateModels(provider Provider, groupsSeen map[string]struct{}) error {
	seen := make(map[string]struct{}, len(provider.Models))
	for _, configured := range provider.Models {
		if configured.ID == "" || !canonicalString(configured.ID) {
			return fmt.Errorf("models config: provider %q model id is required", provider.ID)
		}
		if configured.DisplayName == "" || !canonicalString(configured.DisplayName) {
			return fmt.Errorf("models config: model %q display name must be normalized and non-empty", configured.ID)
		}
		if _, exists := groupsSeen[configured.GroupID]; !exists {
			return fmt.Errorf("models config: model %q for provider %q references unknown group %q", configured.ID, provider.ID, configured.GroupID)
		}
		if _, exists := seen[configured.ID]; exists {
			return fmt.Errorf("models config: duplicate model %q for provider %q", configured.ID, provider.ID)
		}
		seen[configured.ID] = struct{}{}
		switch configured.APIFormat {
		case APIFormatAnthropicMessages, APIFormatOpenAIResponses, APIFormatOpenAIChatCompletions:
		default:
			return fmt.Errorf("models config: model %q has unsupported API format %q", configured.ID, configured.APIFormat)
		}
		if err := validateCapabilities(configured); err != nil {
			return fmt.Errorf("models config: model %q: %w", configured.ID, err)
		}
	}
	return nil
}

func validateCapabilities(configured Model) error {
	switch configured.Thinking.Mode {
	case "", "adaptive":
		if configured.Thinking.BudgetTokens != 0 {
			return errors.New("thinking budget is only supported in enabled mode")
		}
	case "enabled":
		if configured.Thinking.BudgetTokens < 1024 || configured.Thinking.BudgetTokens >= configured.MaxOutputTokens {
			return errors.New("thinking budget must be at least 1024 and less than max output tokens")
		}
	default:
		return errors.New("unsupported or non-canonical thinking mode")
	}
	if configured.APIFormat != APIFormatAnthropicMessages && configured.Thinking != (ModelThinking{}) {
		return errors.New("thinking configuration requires anthropic_messages")
	}
	if configured.APIFormat == APIFormatAnthropicMessages {
		for _, level := range configured.ReasoningLevels {
			if !slices.Contains([]string{
				"off",
				"low",
				"medium",
				"high",
				"xhigh",
				"max",
			}, level) {
				return errors.New("unsupported Anthropic effort level")
			}
			if level == "off" && !configured.Thinking.SupportsDisabled {
				return errors.New("off reasoning requires disabled thinking capability")
			}
		}
	}
	if err := validateReasoningLevels(configured.ReasoningLevels, configured.DefaultReasoningLevel); err != nil {
		return err
	}
	if configured.ContextWindow <= 0 || configured.MaxOutputTokens <= 0 || configured.MaxOutputTokens >= configured.ContextWindow {
		return errors.New("model capability limits are invalid")
	}
	return nil
}

func validateReasoningLevels(levels []string, defaultLevel string) error {
	if len(levels) == 0 {
		if defaultLevel != "" {
			return errors.New("reasoning default requires levels")
		}
		return nil
	}
	seen := make(map[string]struct{}, len(levels))
	for _, level := range levels {
		if level == "" || !canonicalString(level) {
			return errors.New("reasoning levels must be normalized and non-empty")
		}
		if _, exists := seen[level]; exists {
			return fmt.Errorf("duplicate reasoning level %q", level)
		}
		seen[level] = struct{}{}
	}
	if !slices.Contains(levels, defaultLevel) {
		return fmt.Errorf("reasoning default %q is not supported", defaultLevel)
	}
	return nil
}

// validateBaseURL 只读校验 canonical 端点；地址必须使用 HTTP 或 HTTPS，不能包含凭据、查询参数或片段。
func validateBaseURL(value string) error {
	if value == "" {
		return errors.New("provider base URL is required")
	}
	if !canonicalString(value) || strings.HasSuffix(value, "/") {
		return errors.New("provider base URL must be normalized")
	}
	parsed, err := http.NewRequest(http.MethodGet, value, nil)
	if err != nil || parsed.URL == nil || parsed.URL.User != nil || parsed.URL.RawQuery != "" || parsed.URL.Fragment != "" {
		return errors.New("provider base URL must be an absolute URL without credentials, query, or fragment")
	}
	if parsed.URL.Scheme != "http" && parsed.URL.Scheme != "https" || parsed.URL.Host == "" {
		return errors.New("provider base URL must use http or https")
	}
	return nil
}

// validateProxyURL 只读校验 canonical 代理地址，空值表示不覆盖代理；允许认证信息，不能带路径、查询参数或片段。
func validateProxyURL(value string) error {
	if value == "" {
		return nil
	}
	if !canonicalString(value) {
		return errors.New("provider proxy URL must be normalized")
	}
	parsed, err := url.Parse(value)
	if err != nil || parsed == nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New("provider proxy URL must be an absolute HTTP or HTTPS URL without a path, query, or fragment")
	}
	return nil
}

// canonicalString 只检查首尾空白，不构造清洗后的字符串。
func canonicalString(value string) bool {
	first, _ := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)
	return !unicode.IsSpace(first) && !unicode.IsSpace(last)
}
