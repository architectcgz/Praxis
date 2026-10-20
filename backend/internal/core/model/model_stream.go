package model

import (
	"context"
	appcontext "praxis/internal/core/context"
)

// ModelStreamEventKind 标识模型 Provider 适配器产生的流事件类型。
type ModelStreamEventKind string

const (
	StreamTextDelta ModelStreamEventKind = "text_delta"
	// StreamThinkingDelta 承载可供展示的推理文本增量，不用于续传推理状态。
	StreamThinkingDelta ModelStreamEventKind = "thinking_delta"
	StreamToolCall      ModelStreamEventKind = "tool_call"
	// StreamReasoningState 承载推理状态，Decoder 只校验载荷，模型请求边界绑定来源身份；不向观察者展示。
	StreamReasoningState ModelStreamEventKind = "reasoning_state"
	StreamUsage          ModelStreamEventKind = "usage"
	StreamComplete       ModelStreamEventKind = "complete"
	StreamError          ModelStreamEventKind = "error"
)

// ModelStopReason 是单次模型生成的统一结束语义，不代表整个 Execution 已完成。
type ModelStopReason string

const (
	StopCompleted ModelStopReason = "completed"
	StopToolCalls ModelStopReason = "tool_calls"
	StopTruncated ModelStopReason = "truncated"
	StopFiltered  ModelStopReason = "filtered"
	StopPaused    ModelStopReason = "paused"
	StopRefused   ModelStopReason = "refused"
	StopSequence  ModelStopReason = "stop_sequence"
	StopUnknown   ModelStopReason = "unknown"
)

// ModelStreamEvent 是单次模型请求的 Provider 中立流事件。
type ModelStreamEvent struct {
	Kind           ModelStreamEventKind
	Text           string
	ToolCall       ToolCall
	Usage          *ModelUsage
	StopReason     ModelStopReason
	Err            error
	ReasoningState *appcontext.ReasoningState
	// OutputIndex 保存协议输出块顺序，Loop 收集后按此顺序构造历史，不影响实时展示。
	OutputIndex *int
}

// ModelStream 定义模型调用方消费的统一模型流接口。
// Provider 适配器实现此接口，不向调用方暴露协议细节。
type ModelStream interface {
	// Stream 发起模型请求并返回事件通道；请求无效或连接失败时返回错误。
	// 调用方提前停止消费时必须取消 context，以释放 Provider 流资源。
	Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}
