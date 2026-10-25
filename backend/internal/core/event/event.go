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
	EntriesAppended      Kind = "entries_appended"
	TurnStarted          Kind = "turn_started"
	ProviderWaiting      Kind = "provider_waiting"
	TextDelta            Kind = "text_delta"
	ThinkingDelta        Kind = "thinking_delta"
	ToolCall             Kind = "tool_call"
	ToolApprovalRequired Kind = "tool_approval_required"
	ToolResult           Kind = "tool_result"
	TurnCompleted        Kind = "turn_completed"
	ModelUsage           Kind = "model_usage"
	ExecutionEnded       Kind = "execution_ended"
	RequestCanceled      Kind = "request_canceled"
	OperationTiming      Kind = "operation_timing"
	Error                Kind = "error"
)

// Event 是进程内业务通知，当前内存消息与执行状态是运行时事实。
// 订阅者只读事件；重新连接后应查询业务状态恢复视图。
type Event struct {
	// Transient 表示条目只属于当前 Run；无论取值如何，事件都不表示已落盘。
	Transient      bool
	Kind           Kind
	Entries        []sessionentry.ContextEntry
	SessionID      identity.SessionID
	AgentID        identity.AgentID
	ExecutionID    identity.AgentExecutionID
	Outcome        string
	FailureCode    string
	FailureMessage string
	TurnID         identity.TurnID
	StopReason     providerapi.StopReason
	Text           string
	CallID         string
	Name           string
	Input          json.RawMessage
	Result         string
	IsError        bool
	Error          string
	Timing         *timing.View
	Usage          *providerapi.Usage
}
