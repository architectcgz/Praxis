package contracts

import (
	"bytes"
	"encoding/json"
	"errors"
)

const (
	ProtocolOpenAIResponses   = "openai_responses"
	ProtocolAnthropicMessages = "anthropic_messages"
	ReasoningStateVersion     = 2
	MaxReasoningStateBytes    = 1024 * 1024
)

// ReasoningState 保存模型生成的推理状态，供后续请求按来源协议原样续传；不用于展示，也不进行跨协议转换。
// Version 是内部载荷契约版本，Payload 仅允许大小受限的 JSON 对象。
type ReasoningState struct {
	Protocol string
	Version  int
	Payload  json.RawMessage
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

// Validate 只读校验版本、来源和载荷边界；协议内容由对应 adapter 校验。
func (s *ReasoningState) Validate() error {
	if s == nil || s.Version != ReasoningStateVersion {
		return errors.New("invalid reasoning state version")
	}
	if s.Protocol != ProtocolOpenAIResponses && s.Protocol != ProtocolAnthropicMessages {
		return errors.New("unsupported reasoning state protocol")
	}
	if len(s.Payload) == 0 || len(s.Payload) > MaxReasoningStateBytes || !json.Valid(s.Payload) {
		return errors.New("invalid or oversized reasoning state payload")
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(s.Payload, &object); err != nil || object == nil {
		return errors.New("reasoning state payload must be an object")
	}
	return nil
}
