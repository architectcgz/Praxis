package event

import (
	"encoding/json"
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/agent/run/timing"
	providerapi "praxis/internal/core/provider"
	sessionmodel "praxis/internal/core/session"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
	"time"
)

type Kind string

const (
	KindSessionConfigReloaded Kind = "session_config_reloaded"
	KindEntriesAppended       Kind = "entries_appended"
	KindTurnStarted           Kind = "turn_started"
	KindModelRequestStarted   Kind = "model_request_started"
	KindModelRequestFinished  Kind = "model_request_finished"
	KindTextDelta             Kind = "text_delta"
	KindThinkingDelta         Kind = "thinking_delta"
	KindToolCall              Kind = "tool_call"
	KindToolStarted           Kind = "tool_started"
	KindToolApprovalRequired  Kind = "tool_approval_required"
	KindToolResult            Kind = "tool_result"
	KindTurnCompleted         Kind = "turn_completed"
	KindRunStarted            Kind = "run_started"
	KindRunInterruptRequested Kind = "run_interrupt_requested"
	KindRunEnded              Kind = "run_ended"
	KindSubagentStarted       Kind = "subagent_started"
	KindSubagentReleased      Kind = "subagent_released"
	KindTaskQueueChanged      Kind = "task_queue_changed"
	KindRequestCanceled       Kind = "request_canceled"
	KindError                 Kind = "error"
)

// EventSource 标识事件来源的会话、分支、执行和模型轮次；不适用的标识留空。
type EventSource struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID
	BranchID  identity.BranchID
	// BindingID 标识页面的分支写入绑定，关联该绑定的队列和执行生命周期。
	BindingID string
	RunID     identity.RunID
	TurnID    identity.TurnID
}

// SourceForRun 在事件发布边界读取执行归属，不让 Run 依赖通知协议。
func SourceForRun(run *agentrun.Run, turnID identity.TurnID) EventSource {
	record := run.Snapshot()
	return EventSource{
		SessionID: record.SessionID,
		AgentID:   record.AgentID,
		BranchID:  record.BranchID,
		BindingID: run.BindingID,
		RunID:     record.ID,
		TurnID:    turnID,
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
}

func (EntriesAppended) Kind() Kind { return KindEntriesAppended }
func (EntriesAppended) isPayload() {}

type SessionConfigReloaded struct{}

func (SessionConfigReloaded) Kind() Kind { return KindSessionConfigReloaded }
func (SessionConfigReloaded) isPayload() {}

type TurnStarted struct{}

func (TurnStarted) Kind() Kind { return KindTurnStarted }
func (TurnStarted) isPayload() {}

type ModelRequestStarted struct {
	Request agentrun.ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestStarted) Kind() Kind { return KindModelRequestStarted }
func (ModelRequestStarted) isPayload() {}

type ModelRequestFinished struct {
	Request agentrun.ModelRequestRecord
	Timing  *timing.Record
}

func (ModelRequestFinished) Kind() Kind { return KindModelRequestFinished }
func (ModelRequestFinished) isPayload() {}

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
	CallID    string
	Name      string
	Arguments json.RawMessage
}

func (ToolCall) Kind() Kind { return KindToolCall }
func (ToolCall) isPayload() {}

type ToolStarted struct {
	CallID string
	Timing *timing.Record
}

func (ToolStarted) Kind() Kind { return KindToolStarted }
func (ToolStarted) isPayload() {}

type ToolApprovalRequired struct {
	CallID string
}

func (ToolApprovalRequired) Kind() Kind { return KindToolApprovalRequired }
func (ToolApprovalRequired) isPayload() {}

type ToolResult struct {
	CallID  string
	Result  string
	IsError bool
	Timing  *timing.Record
}

func (ToolResult) Kind() Kind { return KindToolResult }
func (ToolResult) isPayload() {}

type TurnCompleted struct {
	StopReason providerapi.StopReason
}

func (TurnCompleted) Kind() Kind { return KindTurnCompleted }
func (TurnCompleted) isPayload() {}

type RunStarted struct {
	Run agentrun.RunRecord
}

func (RunStarted) Kind() Kind { return KindRunStarted }
func (RunStarted) isPayload() {}

type RunInterruptRequested struct{ AppliedAt time.Time }

func (RunInterruptRequested) Kind() Kind { return KindRunInterruptRequested }
func (RunInterruptRequested) isPayload() {}

type RunEnded struct {
	Run     agentrun.RunRecord
	State   agentmodel.AgentState
	Entries []sessionentry.ContextEntry
}

func (RunEnded) Kind() Kind { return KindRunEnded }
func (RunEnded) isPayload() {}

type SubagentStarted struct {
	Name          string
	Run           agentrun.RunRecord
	Entries       []sessionentry.ContextEntry
	ParentAgentID identity.AgentID
	ParentRunID   identity.RunID
	CallID        string
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
	Run     agentrun.RunRecord
	State   agentmodel.AgentState
	Entries []sessionentry.ContextEntry
}

func (RequestCanceled) Kind() Kind { return KindRequestCanceled }
func (RequestCanceled) isPayload() {}

type ErrorOccurred struct {
	Message string
}

func (ErrorOccurred) Kind() Kind { return KindError }
func (ErrorOccurred) isPayload() {}
