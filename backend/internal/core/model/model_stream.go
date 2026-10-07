package model

import (
	"context"

	"praxis/internal/contracts"
)

// ModelStreamEventKind 标识模型 Provider 适配器产生的流事件类型。
type ModelStreamEventKind string

const (
	StreamTextDelta     ModelStreamEventKind = "text_delta"
	StreamThinkingDelta ModelStreamEventKind = "thinking_delta"
	StreamToolCall      ModelStreamEventKind = "tool_call"
	StreamUsage         ModelStreamEventKind = "usage"
	StreamComplete      ModelStreamEventKind = "complete"
	StreamError         ModelStreamEventKind = "error"
)

// ModelStreamEvent 是单次模型请求的 Provider 中立流事件。
type ModelStreamEvent struct {
	Kind       ModelStreamEventKind
	Text       string
	ToolCall   contracts.ToolCall
	Usage      *ModelUsage
	StopReason string
	Err        error
}

// ModelStream 定义模型调用方消费的统一模型流接口。
// Provider 适配器实现此接口，不向调用方暴露协议细节。
type ModelStream interface {
	// Stream 发起模型请求并返回事件通道；请求无效或连接失败时返回错误。
	// 调用方提前停止消费时必须取消 context，以释放 Provider 流资源。
	Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}
