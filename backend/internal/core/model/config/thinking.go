package config

// ModelThinking 冻结模型的 Thinking 配置与禁用能力；Effort 由独立的 ReasoningLevels 表达。
// Mode 为空表示使用服务端默认模式，不据此推断是否会产生 thinking blocks。
type ModelThinking struct {
	Mode             string `json:"mode,omitempty"`
	BudgetTokens     int    `json:"budgetTokens,omitzero"`
	SupportsDisabled bool   `json:"supportsDisabled,omitzero"`
}
