package agent

// AgentConfig 保存 Agent 定义的角色，模型由所属会话的当前选择决定。
type AgentConfig struct {
	Profile AgentProfile `json:"profile,omitempty"`
}
