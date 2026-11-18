package payload

import (
	"praxis/internal/core/entry/record"
	"praxis/internal/core/entry/role"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
)

type Message struct {
	Role     role.Role      `json:"role"`
	Content  record.Content `json:"content"`
	Response *ModelResponse `json:"response,omitempty"`
}

func (Message) isPayload() {}

// ModelResponse 只描述产生本条消息内容的模型响应。
type ModelResponse struct {
	RequestID  identity.ModelRequestID `json:"requestId"`
	StopReason string                  `json:"stopReason,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Timing     *timing.Record          `json:"timing,omitempty"`
}

// Summary 作为历史资料进入模型上下文，不属于某一条对话消息。
type Summary struct {
	Content    record.Content            `json:"content"`
	RequestIDs []identity.ModelRequestID `json:"requestIds"`
	Timing     *timing.Record            `json:"timing,omitempty"`
}

func (Summary) isPayload() {}
