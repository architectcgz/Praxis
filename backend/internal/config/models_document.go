package config

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"praxis/internal/model"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

// ModelGroup 定义模型配置文件中的分组及展示名称。
type ModelGroup struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
}

// Model 保存配置文件中的模型能力与推理等级，不包含运行时凭据。
type Model struct {
	ID                    string              `json:"id"`
	DisplayName           string              `json:"displayName"`
	GroupID               string              `json:"groupId"`
	APIFormat             model.APIFormat     `json:"apiFormat"`
	ContextWindow         int                 `json:"contextWindow"`
	MaxOutputTokens       int                 `json:"maxOutputTokens"`
	ReasoningLevels       []string            `json:"reasoningLevels,omitempty"`
	DefaultReasoningLevel string              `json:"defaultReasoningLevel,omitempty"`
	Thinking              model.ModelThinking `json:"thinking,omitzero"`
}

// ModelProvider 保存配置文件中的模型端点和模型目录；密钥由凭据文件独立管理。
type ModelProvider struct {
	ID          string  `json:"id"`
	DisplayName string  `json:"displayName"`
	BaseURL     string  `json:"baseUrl"`
	ProxyURL    string  `json:"proxyUrl,omitempty"`
	Models      []Model `json:"models"`
}

type ModelReference struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

// ModelDocument 描述 models.json 的完整文件结构，仅用于配置文件读写和目录构造。
type ModelDocument struct {
	Groups       []ModelGroup    `json:"groups"`
	Providers    []ModelProvider `json:"providers"`
	DefaultModel *ModelReference `json:"defaultModel,omitempty"`
}

// validateModelDocument 在配置文件加载边界只读校验完整模型配置文档，不清洗字段或补默认值。
func validateModelDocument(document ModelDocument) error {
	groupsSeen, err := validateGroups(document.Groups)
	if err != nil {
		return err
	}
	if err := validateProviders(document.Providers, groupsSeen); err != nil {
		return err
	}
	if ref := document.DefaultModel; ref != nil {
		found := slices.ContainsFunc(document.Providers, func(provider ModelProvider) bool {
			return provider.ID == ref.ProviderID && slices.ContainsFunc(provider.Models, func(candidate Model) bool {
				return candidate.ID == ref.ModelID
			})
		})
		if !found {
			return fmt.Errorf("models config: defaultModel references unknown model %q for provider %q", ref.ModelID, ref.ProviderID)
		}
	}
	return nil
}

func validateGroups(groups []ModelGroup) (map[string]struct{}, error) {
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

func validateProviders(providers []ModelProvider, groupsSeen map[string]struct{}) error {
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
			return fmt.Errorf(
				"models config: provider %q display name must be normalized and non-empty",
				provider.ID,
			)
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

func validateModels(provider ModelProvider, groupsSeen map[string]struct{}) error {
	seen := make(map[string]struct{}, len(provider.Models))
	for _, configured := range provider.Models {
		if configured.ID == "" || !canonicalString(configured.ID) {
			return fmt.Errorf("models config: provider %q model id is required", provider.ID)
		}
		if configured.DisplayName == "" || !canonicalString(configured.DisplayName) {
			return fmt.Errorf(
				"models config: model %q display name must be normalized and non-empty",
				configured.ID,
			)
		}
		if _, exists := groupsSeen[configured.GroupID]; !exists {
			return fmt.Errorf(
				"models config: model %q for provider %q references unknown group %q",
				configured.ID,
				provider.ID,
				configured.GroupID,
			)
		}
		if _, exists := seen[configured.ID]; exists {
			return fmt.Errorf("models config: duplicate model %q for provider %q", configured.ID, provider.ID)
		}
		seen[configured.ID] = struct{}{}
		if !configured.APIFormat.Valid() {
			return fmt.Errorf(
				"models config: model %q has unsupported API format %q",
				configured.ID,
				configured.APIFormat,
			)
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
		if configured.Thinking.BudgetTokens < 1024 ||
			configured.Thinking.BudgetTokens >= configured.MaxOutputTokens {
			return errors.New("thinking budget must be at least 1024 and less than max output tokens")
		}
	default:
		return errors.New("unsupported or non-canonical thinking mode")
	}
	if configured.APIFormat != model.APIFormatAnthropicMessages &&
		configured.Thinking != (model.ModelThinking{}) {
		return errors.New("thinking configuration requires anthropic_messages")
	}
	if configured.APIFormat == model.APIFormatAnthropicMessages {
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
	if configured.ContextWindow <= 0 ||
		configured.MaxOutputTokens <= 0 ||
		configured.MaxOutputTokens >= configured.ContextWindow {
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
	if err != nil ||
		parsed.URL == nil ||
		parsed.URL.User != nil ||
		parsed.URL.RawQuery != "" ||
		parsed.URL.Fragment != "" {
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
	if err != nil ||
		parsed == nil ||
		parsed.Host == "" ||
		parsed.Path != "" ||
		parsed.RawQuery != "" ||
		parsed.Fragment != "" ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return errors.New(
			"provider proxy URL must be an absolute HTTP or HTTPS URL without a path, query, or fragment",
		)
	}
	return nil
}

// canonicalString 只检查首尾空白，不构造清洗后的字符串。
func canonicalString(value string) bool {
	first, _ := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)
	return !unicode.IsSpace(first) && !unicode.IsSpace(last)
}
