package dto

// ProviderConfigOption 携带可编辑的 Provider 元数据和密钥状态。
type ProviderConfigOption struct {
	ID           string `json:"id" validate:"config_id"`
	ProviderName string `json:"providerName" validate:"required,notblank"`
	BaseURL      string `json:"baseURL" validate:"required,model_base_url"`
	ProxyURL     string `json:"proxyURL" validate:"omitempty,model_proxy_url"`
	HasAPIKey    bool   `json:"hasAPIKey"`
}

type GroupConfigOption struct {
	ID          string `json:"id" validate:"config_id"`
	DisplayName string `json:"displayName" validate:"required,notblank"`
}

// ModelConfigOption 携带设置界面编辑模型所需的全部字段。
type ModelConfigOption struct {
	ProviderID            string        `json:"providerId" validate:"config_id"`
	ModelID               string        `json:"modelId" validate:"id"`
	Label                 string        `json:"label" validate:"required,notblank"`
	GroupID               string        `json:"groupId" validate:"config_id"`
	APIFormat             string        `json:"apiFormat" validate:"oneof=anthropic_messages openai_responses openai_chat_completions"`
	ContextWindow         int           `json:"contextWindow" validate:"gt=0"`
	MaxOutputTokens       int           `json:"maxOutputTokens" validate:"gt=0,ltfield=ContextWindow"`
	ReasoningLevels       []string      `json:"reasoningLevels" validate:"unique,dive,id"`
	DefaultReasoningLevel string        `json:"defaultReasoningLevel" validate:"omitempty,id"`
	Thinking              ModelThinking `json:"thinking,omitzero"`
}

// ModelThinking 是模型设置协议，不暴露业务配置对象。
type ModelThinking struct {
	Mode             string `json:"mode,omitempty" validate:"omitempty,oneof=adaptive enabled"`
	BudgetTokens     int    `json:"budgetTokens,omitzero" validate:"gte=0"`
	SupportsDisabled bool   `json:"supportsDisabled,omitzero"`
}

// ModelConfigDocument 表示可编辑的模型注册表文档。
type ModelConfigDocument struct {
	Groups    []GroupConfigOption    `json:"groups"`
	Providers []ProviderConfigOption `json:"providers"`
	Models    []ModelConfigOption    `json:"models"`
}

type SaveModelConfigRequest struct {
	Groups    []GroupConfigOption    `json:"groups" validate:"dive"`
	Providers []ProviderConfigOption `json:"providers" validate:"dive"`
	Models    []ModelConfigOption    `json:"models" validate:"dive"`
}

// SaveModelConfigResponse 通过 validationError 报告用户可修正的配置问题。
// 这些问题不属于调用失败，不映射为 binding 错误码，以便界面展示具体修正信息。
type SaveModelConfigResponse struct {
	Saved           bool   `json:"saved"`
	ValidationError string `json:"validationError,omitempty"`
}
