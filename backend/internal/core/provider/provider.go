package provider

import (
	"context"
	"encoding/json"
	appcontext "praxis/internal/core/context"
	modelapi "praxis/internal/core/model"
	modelusage "praxis/internal/core/model/usage"
	"praxis/internal/core/tool"
	"praxis/internal/identity"
)

// ToolCall 是 Provider 与模型调用方共享的纯工具请求数据。
type ToolCall struct {
	ID   string
	Name tool.ToolName
	// Arguments 保留模型返回的参数原文，合法性由工具执行入口判断。
	Arguments string
}

// Request 是发送给 Provider 的一次完整模型上下文请求，不包含凭据字段。
type Request struct {
	ID    identity.ModelRequestID
	RunID identity.RunID
	// PromptCacheKey 用于 Provider 的 Prompt Cache 路由，留空时不发送。
	PromptCacheKey  string
	Context         appcontext.ModelContext
	Model           modelapi.Config
	MaxOutputTokens int
	Tools           []tool.ToolDefinition
	TurnID          identity.TurnID
	// OutputSchema 非空时启用严格 JSON Schema 输出；为空时不指定输出格式。
	OutputSchema *OutputSchema
}

// OutputSchema 定义结构化输出的名称与 JSON Schema，不携带 Provider 协议字段。
type OutputSchema struct {
	Name   string
	Schema json.RawMessage
}

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

// StopReason 是单次模型生成的统一结束语义，不代表整个 Run 已完成。
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
	Usage          *modelusage.Usage
	StopReason     StopReason
	Err            error
	ReasoningState *modelapi.ReasoningState
	// OutputIndex 保存协议输出块顺序，Loop 收集后按此顺序构造历史，不影响实时展示。
	OutputIndex *int
}

// Provider 定义模型调用方消费的统一模型流接口。
// Provider 适配器实现此接口，不向调用方暴露协议细节。
// Provider 每次 Stream 只发起一次尝试；取消后释放流资源并关闭通道。
// 消费者提前停止处理时应取消 Context，并消费剩余事件直到通道关闭。
type Provider interface {
	// Stream 发起模型请求并返回事件通道；请求无效或连接失败时返回错误。
	// 返回 nil 错误时，事件通道必须非 nil。
	// 调用方提前停止消费时必须取消 context，以释放 Provider 流资源。
	Stream(context.Context, Request) (<-chan Event, error)
}
