package contracts

import "errors"

// ModelThinking 冻结模型的 Thinking 配置与禁用能力；Effort 由独立的 ReasoningLevels 表达。
// Mode 为空表示使用服务端默认模式，不据此推断是否会产生 thinking blocks。
type ModelThinking struct {
	Mode             string `json:"mode,omitempty"`
	BudgetTokens     int    `json:"budgetTokens,omitzero"`
	SupportsDisabled bool   `json:"supportsDisabled,omitzero"`
}

// Validate 只读校验配置；enabled 的预算必须给最终答案留出输出空间。
func (s ModelThinking) Validate(maxOutputTokens int) error {
	switch s.Mode {
	case "", "adaptive":
		if s.BudgetTokens != 0 {
			return errors.New("thinking budget is only supported in enabled mode")
		}
	case "enabled":
		if s.BudgetTokens < 1024 || s.BudgetTokens >= maxOutputTokens {
			return errors.New("thinking budget must be at least 1024 and less than max output tokens")
		}
	default:
		return errors.New("unsupported or non-canonical thinking mode")
	}
	return nil
}
