package dto

// ModelThinking 是模型设置协议，不暴露业务配置对象。
type ModelThinking struct {
	Mode             string `json:"mode,omitempty" validate:"omitempty,oneof=adaptive enabled"`
	BudgetTokens     int    `json:"budgetTokens,omitzero" validate:"gte=0"`
	SupportsDisabled bool   `json:"supportsDisabled,omitzero"`
}
