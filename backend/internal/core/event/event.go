package event

import (
	"encoding/json"
	"praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

type Kind string

const (
	KindEntriesAppended      Kind = "entries_appended"
	KindTurnStarted          Kind = "turn_started"
	KindProviderWaiting      Kind = "provider_waiting"
	KindTextDelta            Kind = "text_delta"
	KindThinkingDelta        Kind = "thinking_delta"
	KindToolCall             Kind = "tool_call"
	KindToolApprovalRequired Kind = "tool_approval_required"
	KindToolResult           Kind = "tool_result"
	KindTurnCompleted        Kind = "turn_completed"
	KindModelUsage           Kind = "model_usage"
	KindExecutionEnded       Kind = "execution_ended"
	KindRequestCanceled      Kind = "request_canceled"
	KindOperationTiming      Kind = "operation_timing"
	KindError                Kind = "error"
)

// Scope 标识事件所属的会话、Agent、执行和模型轮次；不适用的标识留空。
type Scope struct {
	SessionID   identity.SessionID
	AgentID     identity.AgentID
	ExecutionID identity.AgentExecutionID
	TurnID      identity.TurnID
}

// Event 是进程内业务通知，不表示已落盘；重新连接后应查询业务状态恢复视图。
// Payload 必须使用本包定义的非空值载荷；订阅者不得修改其中的切片与指针。
type Event struct {
	Scope   Scope
	Payload Payload
}

func (e Event) Kind() Kind { return e.Payload.Kind() }

// Payload 由本包封闭定义，事件种类仅由载荷类型决定。
type Payload interface {
	Kind() Kind
	isPayload()
}

type EntriesAppended struct {
	Entries []sessionentry.ContextEntry
	// Transient 表示条目只属于当前 Run；无论取值如何，事件都不表示已落盘。
	Transient bool
}

func (EntriesAppended) Kind() Kind { return KindEntriesAppended }
func (EntriesAppended) isPayload() {}

type TurnStarted struct{}

func (TurnStarted) Kind() Kind { return KindTurnStarted }
func (TurnStarted) isPayload() {}

type ProviderWaiting struct{}

func (ProviderWaiting) Kind() Kind { return KindProviderWaiting }
func (ProviderWaiting) isPayload() {}

type TextDelta struct {
	Text string
}

func (TextDelta) Kind() Kind { return KindTextDelta }
func (TextDelta) isPayload() {}

type ThinkingDelta struct {
	Text string
}

func (ThinkingDelta) Kind() Kind { return KindThinkingDelta }
func (ThinkingDelta) isPayload() {}

type ToolCall struct {
	CallID string
	Name   string
	Input  json.RawMessage
}

func (ToolCall) Kind() Kind { return KindToolCall }
func (ToolCall) isPayload() {}

type ToolApprovalRequired struct {
	CallID string
	Name   string
	Input  json.RawMessage
}

func (ToolApprovalRequired) Kind() Kind { return KindToolApprovalRequired }
func (ToolApprovalRequired) isPayload() {}

type ToolResult struct {
	CallID  string
	Result  string
	IsError bool
}

func (ToolResult) Kind() Kind { return KindToolResult }
func (ToolResult) isPayload() {}

type TurnCompleted struct {
	StopReason providerapi.StopReason
}

func (TurnCompleted) Kind() Kind { return KindTurnCompleted }
func (TurnCompleted) isPayload() {}

type ModelUsage struct {
	Usage providerapi.Usage
}

func (ModelUsage) Kind() Kind { return KindModelUsage }
func (ModelUsage) isPayload() {}

type ExecutionEnded struct {
	Outcome        string
	FailureCode    string
	FailureMessage string
}

func (ExecutionEnded) Kind() Kind { return KindExecutionEnded }
func (ExecutionEnded) isPayload() {}

type RequestCanceled struct{}

func (RequestCanceled) Kind() Kind { return KindRequestCanceled }
func (RequestCanceled) isPayload() {}

type OperationTiming struct {
	Timing timing.View
}

func (OperationTiming) Kind() Kind { return KindOperationTiming }
func (OperationTiming) isPayload() {}

type ErrorOccurred struct {
	Message string
}

func (ErrorOccurred) Kind() Kind { return KindError }
func (ErrorOccurred) isPayload() {}
