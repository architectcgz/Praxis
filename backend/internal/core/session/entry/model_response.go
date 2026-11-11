package entry

import (
	"praxis/internal/core/agent/run/timing"
	"praxis/internal/identity"
)

// ModelResponse 只描述产生本条文本、思考或工具声明的模型响应。
type ModelResponse struct {
	RequestID  identity.ModelRequestID `json:"requestId"`
	StopReason string                  `json:"stopReason,omitempty"`
	Error      string                  `json:"error,omitempty"`
	Timing     *timing.Record          `json:"timing,omitempty"`
}

// Compression 描述 Run 级摘要及仍需保留的原文，不属于压缩前后任一 Turn。
// 分片摘要可能包含多个模型请求；保留条目通过 ID 关联 Session 中的同一份 Entry。
type Compression struct {
	RequestIDs       []identity.ModelRequestID `json:"requestIds"`
	RetainedEntryIDs []string                  `json:"retainedEntryIds,omitempty"`
	Timing           *timing.Record            `json:"timing,omitempty"`
}
