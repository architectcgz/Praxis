package runtime

import (
	"praxis/internal/contracts"
	toolcontracts "praxis/internal/tools/contracts"

	"context"

	appcontext "praxis/internal/context"
)

// ModelStreamEventKind identifies an event emitted by a model provider adapter.
type ModelStreamEventKind string

const (
	StreamTextDelta     ModelStreamEventKind = "text_delta"
	StreamThinkingDelta ModelStreamEventKind = "thinking_delta"
	StreamToolCall      ModelStreamEventKind = "tool_call"
	StreamComplete      ModelStreamEventKind = "complete"
	StreamError         ModelStreamEventKind = "error"
)

// ModelStreamEvent is a provider-neutral event from one model request.
type ModelStreamEvent struct {
	Kind       ModelStreamEventKind
	Text       string
	ToolCall   toolcontracts.ToolCall
	StopReason string
	Err        error
}

// ModelRequest 是发送给 Provider 的一次完整模型上下文请求，不包含凭据字段。
type ModelRequest struct {
	ExecutionID      contracts.AgentExecutionID
	SessionReference string
	Context          appcontext.ExecutionContext
	Model            contracts.ExecutionModelSnapshot
	MaxOutputTokens  int
	Tools            []toolcontracts.ToolDefinition
	Execution        contracts.RuntimeExecutionSnapshot
	TurnNumber       int
}

// ExecutionModel is the resolved model for one execution turn: the provider-neutral
// stream adapter plus the output token budget to request. It is resolved per turn
// so that configuration and credential changes take effect on the next request.
//
// 模型的上下文窗口不在这里：窗口只用于本地提前拒绝，所有权归 provider adapter
// （它才知道本协议序列化出的请求体）。
type ExecutionModel struct {
	Stream          ModelStream
	MaxOutputTokens int
}

// ModelStream is the provider-neutral streaming contract owned by the core.
// Provider adapters implement it without exposing protocol details inward.
type ModelStream interface {
	Stream(context.Context, ModelRequest) (<-chan ModelStreamEvent, error)
}
