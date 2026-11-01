package event

import (
	"encoding/json"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	"praxis/internal/core/agent/execution/timing"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"time"
)

type Kind string

const (
	KindSessionConfigReloaded   Kind = "session_config_reloaded"
	KindEntriesAppended         Kind = "entries_appended"
	KindTurnStarted             Kind = "turn_started"
	KindProviderWaiting         Kind = "provider_waiting"
	KindTextDelta               Kind = "text_delta"
	KindThinkingDelta           Kind = "thinking_delta"
	KindToolCall                Kind = "tool_call"
	KindToolApprovalRequired    Kind = "tool_approval_required"
	KindToolResult              Kind = "tool_result"
	KindTurnCompleted           Kind = "turn_completed"
	KindModelUsage              Kind = "model_usage"
	KindExecutionStarted        Kind = "execution_started"
	KindExecutionPauseRequested Kind = "execution_pause_requested"
	KindExecutionStopRequested  Kind = "execution_stop_requested"
	KindExecutionEnded          Kind = "execution_ended"
	KindSubagentStarted         Kind = "subagent_started"
	KindSubagentReleased        Kind = "subagent_released"
	KindTaskQueueChanged        Kind = "task_queue_changed"
	KindRequestCanceled         Kind = "request_canceled"
	KindOperationTiming         Kind = "operation_timing"
	KindError                   Kind = "error"
)

// EventSource 标识事件来源的会话、分支、执行和模型轮次；不适用的标识留空。
type EventSource struct {
	SessionID identity.SessionID
	BranchID  identity.BranchID
	// BindingID 标识页面的分支写入绑定，关联该绑定的队列和执行生命周期。
	BindingID   string
	ExecutionID identity.AgentExecutionID
	TurnID      identity.TurnID
}

// SourceForRun 在事件发布边界读取执行归属，不让 Run 依赖通知协议。
func SourceForRun(run *agentexecution.Run, turnID identity.TurnID) EventSource {
	record := run.Snapshot()
	return EventSource{
		SessionID:   record.SessionID,
		BranchID:    record.BranchID,
		BindingID:   run.BindingID,
		ExecutionID: record.ID,
		TurnID:      turnID,
	}
}

// Event 是进程内业务通知，不表示已落盘；重新连接后应查询业务状态恢复视图。
// Payload 必须使用本包定义的非空值载荷；订阅者不得修改其中的切片与指针。
type Event struct {
	Source  EventSource
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

type SessionConfigReloaded struct{}

func (SessionConfigReloaded) Kind() Kind { return KindSessionConfigReloaded }
func (SessionConfigReloaded) isPayload() {}

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

type ExecutionStarted struct {
	Execution agentexecution.ExecutionRecord
}

func (ExecutionStarted) Kind() Kind { return KindExecutionStarted }
func (ExecutionStarted) isPayload() {}

type ExecutionPauseRequested struct{ AppliedAt time.Time }

func (ExecutionPauseRequested) Kind() Kind { return KindExecutionPauseRequested }
func (ExecutionPauseRequested) isPayload() {}

type ExecutionStopRequested struct{ AppliedAt time.Time }

func (ExecutionStopRequested) Kind() Kind { return KindExecutionStopRequested }
func (ExecutionStopRequested) isPayload() {}

type ExecutionEnded struct {
	Execution agentexecution.ExecutionRecord
	State     agentmodel.AgentState
	Entries   []sessionentry.ContextEntry
}

func (ExecutionEnded) Kind() Kind { return KindExecutionEnded }
func (ExecutionEnded) isPayload() {}

type SubagentStarted struct {
	Name              string
	Execution         agentexecution.ExecutionRecord
	Entries           []sessionentry.ContextEntry
	ParentAgentID     identity.AgentID
	ParentExecutionID identity.AgentExecutionID
	CallID            string
}

func (SubagentStarted) Kind() Kind { return KindSubagentStarted }
func (SubagentStarted) isPayload() {}

type SubagentReleased struct{}

func (SubagentReleased) Kind() Kind { return KindSubagentReleased }
func (SubagentReleased) isPayload() {}

type TaskQueueChanged struct{ Tasks []sessionmodel.Task }

func (TaskQueueChanged) Kind() Kind { return KindTaskQueueChanged }
func (TaskQueueChanged) isPayload() {}

type RequestCanceled struct {
	Execution agentexecution.ExecutionRecord
	State     agentmodel.AgentState
	Entries   []sessionentry.ContextEntry
}

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
