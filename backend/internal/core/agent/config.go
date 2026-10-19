package agent

// ModelReference 标识已配置的 Provider 与 Model 组合。
type ModelReference struct {
	ProviderID string `json:"providerId"`
	ModelID    string `json:"modelId"`
}

// AgentConfig 保存一个 Agent 定义的角色和默认模型。
type AgentConfig struct {
	Profile AgentProfile    `json:"profile,omitempty"`
	Model   *ModelReference `json:"model,omitempty"`
}
