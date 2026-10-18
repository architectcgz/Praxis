package contracts

// ModelConfigGroup 是设置页可编辑的模型分组。
type ModelConfigGroup struct {
	ID          string
	DisplayName string
}

// ModelConfigProvider 是可编辑的 Provider 元数据，不包含凭据。
type ModelConfigProvider struct {
	ID             string
	DisplayName    string
	BaseURL        string
	ProxyURL       string
	DefaultModelID string
	HasAPIKey      bool
}

// ModelThinking 描述公开的 Thinking 配置与禁用能力，不暴露内核配置类型。
type ModelThinking struct {
	Mode             string `json:"mode,omitempty"`
	BudgetTokens     int    `json:"budgetTokens,omitzero"`
	SupportsDisabled bool   `json:"supportsDisabled,omitzero"`
}

// ConfiguredModel 是设置页可编辑的模型能力。
type ConfiguredModel struct {
	ProviderID            string
	ModelID               string
	DisplayName           string
	GroupID               string
	APIFormat             string
	ContextWindow         int
	MaxOutputTokens       int
	ReasoningLevels       []string
	DefaultReasoningLevel string
	Thinking              ModelThinking
}

// ModelConfig 是完整的可编辑模型配置，不包含凭据内容。
type ModelConfig struct {
	Groups            []ModelConfigGroup
	DefaultProviderID string
	Providers         []ModelConfigProvider
	Models            []ConfiguredModel
}
