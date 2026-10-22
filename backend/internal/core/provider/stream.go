package provider

import (
	"context"
	sessionentry "praxis/internal/core/session/entry"
)

// EventKind 标识模型 Provider 适配器产生的流事件类型。
type EventKind string

const (
	StreamTextDelta EventKind = "text_delta"
	// StreamThinkingDelta 承载可供展示的推理文本增量，不用于续传推理状态。
	StreamThinkingDelta EventKind = "thinking_delta"
	StreamToolCall      EventKind = "tool_call"
	// StreamReasoningState 承载推理状态，Decoder 只校验载荷，模型请求边界绑定来源身份；不向观察者展示。
	StreamReasoningState EventKind = "reasoning_state"
	StreamUsage          EventKind = "usage"
	StreamComplete       EventKind = "complete"
	StreamError          EventKind = "error"
)

// StopReason 是单次模型生成的统一结束语义，不代表整个 Execution 已完成。
type StopReason string

const (
	StopCompleted StopReason = "completed"
	StopToolCalls StopReason = "tool_calls"
	StopTruncated StopReason = "truncated"
	StopFiltered  StopReason = "filtered"
	StopPaused    StopReason = "paused"
	StopRefused   StopReason = "refused"
	StopSequence  StopReason = "stop_sequence"
	StopUnknown   StopReason = "unknown"
)

// Event 是单次模型请求的 Provider 中立流事件。
type Event struct {
	Kind           EventKind
	Text           string
	ToolCall       ToolCall
	Usage          *Usage
	StopReason     StopReason
	Err            error
	ReasoningState *sessionentry.ReasoningState
	// OutputIndex 保存协议输出块顺序，Loop 收集后按此顺序构造历史，不影响实时展示。
	OutputIndex *int
}

// Provider 定义模型调用方消费的统一模型流接口。
// Provider 适配器实现此接口，不向调用方暴露协议细节。
type Provider interface {
	// Stream 发起模型请求并返回事件通道；请求无效或连接失败时返回错误。
	// 调用方提前停止消费时必须取消 context，以释放 Provider 流资源。
	Stream(context.Context, Request) (<-chan Event, error)
}
