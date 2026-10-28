package model

import (
	"errors"
	"slices"
	"strings"

	modelconfig "praxis/internal/core/model/config"
	sessionentry "praxis/internal/core/session/entry"
)

// Config 保存 Execution 选定并持久化的模型配置。
//
// 这些字段不能在 task 运行期间从当前模型配置重新读取，否则修改配置
// 会改变已经创建的 task 的请求协议和资源边界。
type Config struct {
	ProviderID      string
	ModelID         string
	ReasoningLevel  string
	ReasoningLevels []string                  `json:",omitempty"`
	Thinking        modelconfig.ModelThinking `json:",omitzero"`
	APIFormat       string
	ContextWindow   int
	MaxOutputTokens int
	BaseURL         string
	ProxyURL        string
}

// Clone 返回配置的独立副本，推理能力列表不能被请求接收方修改。
func (s Config) Clone() Config {
	s.ReasoningLevels = slices.Clone(s.ReasoningLevels)
	return s
}

// CanReplayReasoningState 判断完整状态的来源是否与目标 Provider、API 和模型完全一致；nil 状态不可重放。
func (s Config) CanReplayReasoningState(state *sessionentry.ReasoningState) bool {
	return state != nil && state.ProviderID == s.ProviderID && state.Protocol == s.APIFormat && state.ModelID == s.ModelID
}

// Validate 检查对象自身的完整性，不修改或修复输入。
func (s Config) Validate() error {
	seen := map[string]bool{}
	for _, level := range s.ReasoningLevels {
		if level == "" || level != strings.TrimSpace(level) || seen[level] {
			return errors.New("execution model reasoning capabilities must be canonical and unique")
		}
		seen[level] = true
		if s.APIFormat == string(modelconfig.APIFormatAnthropicMessages) && (!slices.Contains([]string{
			"off",
			"low",
			"medium",
			"high",
			"xhigh",
			"max",
		}, level) || level == "off" && !s.Thinking.SupportsDisabled) {
			return errors.New("execution model has an unsupported Anthropic effort level")
		}
	}
	switch s.Thinking.Mode {
	case "", "adaptive":
		if s.Thinking.BudgetTokens != 0 {
			return errors.New("thinking budget is only supported in enabled mode")
		}
	case "enabled":
		if s.Thinking.BudgetTokens < 1024 || s.Thinking.BudgetTokens >= s.MaxOutputTokens {
			return errors.New("thinking budget must be at least 1024 and less than max output tokens")
		}
	default:
		return errors.New("unsupported or non-canonical thinking mode")
	}
	if s.APIFormat != string(modelconfig.APIFormatAnthropicMessages) && s.Thinking != (modelconfig.ModelThinking{}) {
		return errors.New("thinking configuration requires anthropic_messages")
	}
	if s.ReasoningLevel != "" && !slices.Contains(s.ReasoningLevels, s.ReasoningLevel) {
		return errors.New("execution reasoning level is not supported by frozen capabilities")
	}
	if s.ProviderID == "" || s.ModelID == "" || s.ProviderID != strings.TrimSpace(s.ProviderID) || s.ModelID != strings.TrimSpace(s.ModelID) || s.ReasoningLevel != strings.TrimSpace(s.ReasoningLevel) {
		return errors.New("execution model identity fields must be canonical and nonempty")
	}
	switch s.APIFormat {
	case string(modelconfig.APIFormatAnthropicMessages), string(modelconfig.APIFormatOpenAIResponses), string(modelconfig.APIFormatOpenAIChatCompletions):
	default:
		return errors.New("execution model API format is invalid")
	}
	if s.ContextWindow <= 0 || s.MaxOutputTokens <= 0 || s.MaxOutputTokens >= s.ContextWindow {
		return errors.New("execution model capability limits are invalid")
	}
	if _, err := modelconfig.ValidateModelBaseURL(s.BaseURL); err != nil {
		return err
	}
	_, err := modelconfig.ValidateModelProxyURL(s.ProxyURL)
	return err
}
