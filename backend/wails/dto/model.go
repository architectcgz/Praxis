package dto

type ModelOption struct {
	ProviderID            string   `json:"providerId"`
	ModelID               string   `json:"modelId"`
	Label                 string   `json:"label"`
	ProviderName          string   `json:"providerName"`
	ReasoningLevels       []string `json:"reasoningLevels"`
	DefaultReasoningLevel string   `json:"defaultReasoningLevel"`
}

// SelectModelRequest 更新指定会话的选择；模型字段全空表示清除该会话的选择。
type SelectModelRequest struct {
	SessionID      string `json:"sessionId" validate:"session_id"`
	ProviderID     string `json:"providerId" validate:"required_with=ModelID ReasoningLevel,omitempty,config_id"`
	ModelID        string `json:"modelId" validate:"required_with=ProviderID ReasoningLevel,omitempty,id"`
	ReasoningLevel string `json:"reasoningLevel" validate:"omitempty,id"`
}

// ModelSelection 只返回会话选择的公开字段，不包含完整模型配置。
type ModelSelection struct {
	ProviderID     string `json:"providerId"`
	ModelID        string `json:"modelId"`
	ReasoningLevel string `json:"reasoningLevel"`
}
