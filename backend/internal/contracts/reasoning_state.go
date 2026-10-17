package contracts

import (
	"bytes"
	"encoding/json"
)

const (
	ProtocolOpenAIResponses   = "openai_responses"
	ProtocolAnthropicMessages = "anthropic_messages"
	MaxReasoningStateBytes    = 1024 * 1024
)

// ReasoningState 保存模型生成的推理状态，仅供相同 Provider、协议与模型原样续传；不用于展示或转换为文本。
// 此类型只承载数据；协议解析、来源绑定和持久化校验分别由对应边界负责。
type ReasoningState struct {
	ProviderID string
	Protocol   string
	ModelID    string
	Payload    json.RawMessage
}

// Clone 深拷贝载荷；nil 状态仍返回 nil，避免持久化与观察者共享可变字节。
func (s *ReasoningState) Clone() *ReasoningState {
	if s == nil {
		return nil
	}
	copy := *s
	copy.Payload = bytes.Clone(s.Payload)
	return &copy
}
