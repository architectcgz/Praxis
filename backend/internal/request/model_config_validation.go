package request

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"unicode"
	"unicode/utf8"

	modelconfig "praxis/internal/core/model/config"
)

var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]*$`)

// ValidateModelConfig 在请求或配置文件输入边界只读校验完整配置，不清洗字段或补默认值。
func ValidateModelConfig(config modelconfig.Config) error {
	groupsSeen, err := validateGroups(config.Groups)
	if err != nil {
		return err
	}
	if err := validateProviders(config.Providers, groupsSeen); err != nil {
		return err
	}
	if config.DefaultProviderID == "" {
		if len(config.Providers) > 0 {
			return errors.New("models config: default provider is required")
		}
		return nil
	}
	if !slices.ContainsFunc(config.Providers, func(provider modelconfig.Provider) bool {
		return provider.ID == config.DefaultProviderID
	}) {
		return fmt.Errorf("models config: default provider %q is not configured", config.DefaultProviderID)
	}
	return nil
}

func validateGroups(groups []modelconfig.Group) (map[string]struct{}, error) {
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

func validateProviders(providers []modelconfig.Provider, groupsSeen map[string]struct{}) error {
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
		if _, err := ValidateModelBaseURL(provider.BaseURL); err != nil {
			return fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		if _, err := ValidateModelProxyURL(provider.ProxyURL); err != nil {
			return fmt.Errorf("models config: provider %q: %w", provider.ID, err)
		}
		if err := validateModels(provider, groupsSeen); err != nil {
			return err
		}
		if provider.DefaultModelID != "" && !slices.ContainsFunc(provider.Models, func(configured modelconfig.Model) bool {
			return configured.ID == provider.DefaultModelID
		}) {
			return fmt.Errorf("models config: provider %q default model %q is not configured", provider.ID, provider.DefaultModelID)
		}
	}
	return nil
}

func validateModels(provider modelconfig.Provider, groupsSeen map[string]struct{}) error {
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
		case modelconfig.APIFormatAnthropicMessages, modelconfig.APIFormatOpenAIResponses, modelconfig.APIFormatOpenAIChatCompletions:
		default:
			return fmt.Errorf("models config: model %q has unsupported API format %q", configured.ID, configured.APIFormat)
		}
		if err := validateCapabilities(configured); err != nil {
			return fmt.Errorf("models config: model %q: %w", configured.ID, err)
		}
	}
	return nil
}

func validateCapabilities(configured modelconfig.Model) error {
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
	if configured.APIFormat != modelconfig.APIFormatAnthropicMessages && configured.Thinking != (modelconfig.ModelThinking{}) {
		return errors.New("thinking configuration requires anthropic_messages")
	}
	if configured.APIFormat == modelconfig.APIFormatAnthropicMessages {
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

// canonicalString 只检查首尾空白，不构造清洗后的字符串。
func canonicalString(value string) bool {
	first, _ := utf8.DecodeRuneInString(value)
	last, _ := utf8.DecodeLastRuneInString(value)
	return !unicode.IsSpace(first) && !unicode.IsSpace(last)
}
