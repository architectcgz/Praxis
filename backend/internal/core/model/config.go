package model

import (
	modelconfig "praxis/internal/core/model/config"
	sessionentry "praxis/internal/core/session/entry"
)

// Config 保存 Run 选定并持久化的只读模型配置，能力列表在执行链路中共享。
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

// CanReplayReasoningState 判断完整状态的来源是否与目标 Provider、API 和模型完全一致；nil 状态不可重放。
func (s Config) CanReplayReasoningState(state *sessionentry.ReasoningState) bool {
	return state != nil && state.ProviderID == s.ProviderID && state.Protocol == s.APIFormat && state.ModelID == s.ModelID
}
