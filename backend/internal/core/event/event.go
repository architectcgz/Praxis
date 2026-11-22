package event

import (
	agentmodel "praxis/internal/core/agent"
	sessionentry "praxis/internal/core/entry"
	modelapi "praxis/internal/core/model"
	"praxis/internal/core/security"
	sessionmodel "praxis/internal/core/session"
	taskmodel "praxis/internal/core/task"
	"praxis/internal/core/timing"
	"praxis/internal/identity"
	"time"
)

type Kind string

const (
	KindSessionConfigReloaded   Kind = "session_config_reloaded"
	KindSessionTitleChanged     Kind = "session_title_changed"
	KindEntriesAppended         Kind = "entries_appended"
	KindTurnStarted             Kind = "turn_started"
	KindModelRequestStarted     Kind = "model_request_started"
	KindModelRequestCompleted   Kind = "model_request_completed"
	KindModelRequestFailed      Kind = "model_request_failed"
	KindModelRequestInterrupted Kind = "model_request_interrupted"
	KindAgentStateChanged       Kind = "agent_state_changed"
	KindTurnFailed              Kind = "turn_failed"
	KindTurnInterrupted         Kind = "turn_interrupted"
	KindTextDelta               Kind = "text_delta"
	KindThinkingDelta           Kind = "thinking_delta"
	KindToolCall                Kind = "tool_call"
	KindToolStarted             Kind = "tool_started"
	KindToolApprovalRequired    Kind = "tool_approval_required"
	KindToolApprovalResolved    Kind = "tool_approval_resolved"
	KindToolResult              Kind = "tool_result"
	KindTurnCompleted           Kind = "turn_completed"
	KindRunStarted              Kind = "run_started"
	KindRunEnding               Kind = "run_ending"
	KindRunEnded                Kind = "run_ended"
	KindSubagentStarted         Kind = "subagent_started"
	KindSubagentReleased        Kind = "subagent_released"
	KindTaskQueueChanged        Kind = "task_queue_changed"
	KindTaskQueueStorageFailed  Kind = "task_queue_storage_failed"
	KindError                   Kind = "error"
)

// EventSource 标识事件来源的会话、执行和模型轮次；不适用的标识留空。
type EventSource struct {
	SessionID identity.SessionID
	AgentID   identity.AgentID

	RunID  identity.RunID
	TurnID identity.TurnID
}

// Event 是进程内业务通知，不表示已落盘；重新连接后应查询业务状态恢复视图。
// Payload 必须使用本包定义的非空值载荷；订阅者不得修改其中的切片与指针。
type Event struct {
	Source  EventSource
	Payload Payload
}

func (e Event) Kind() Kind { return e.Payload.Kind() }

// Payload 由本包封闭定义，事件种类由载荷类型决定。
type Payload interface {
	Kind() Kind
	isPayload()
}

type EntriesAppended struct {
	Entries []*sessionentry.Entry
}

func (EntriesAppended) Kind() Kind { return KindEntriesAppended }
func (EntriesAppended) isPayload() {}

type SessionConfigReloaded struct {
	Models        []modelapi.Option
	ModelChoice   sessionmodel.ModelChoice
	GlobalMode    security.ApprovalMode
	SessionMode   *security.ApprovalMode
	EffectiveMode security.ApprovalMode
}

func (SessionConfigReloaded) Kind() Kind { return KindSessionConfigReloaded }
func (SessionConfigReloaded) isPayload() {}

type SessionTitleChanged struct {
	Title     string
	UpdatedAt time.Time
}

func (SessionTitleChanged) Kind() Kind { return KindSessionTitleChanged }
func (SessionTitleChanged) isPayload() {}

type TurnStarted struct{}

func (TurnStarted) Kind() Kind { return KindTurnStarted }
func (TurnStarted) isPayload() {}

// ModelRequestInfo 是各阶段共用的请求信息，HistoryEnd 用于与分页结果合并。
type ModelRequestInfo struct {
	RequestID  identity.ModelRequestID
	Kind       sessionentry.ModelRequestKind
	RetryCount int
	CreatedAt  time.Time
	HistoryEnd int64
	Timing     *timing.Record
}

type ModelRequestStarted struct{ Info ModelRequestInfo }

func (ModelRequestStarted) Kind() Kind { return KindModelRequestStarted }
func (ModelRequestStarted) isPayload() {}

type ModelRequestCompleted struct {
	Info       ModelRequestInfo
	StopReason string
	Usage      *modelapi.Usage
}

func (ModelRequestCompleted) Kind() Kind { return KindModelRequestCompleted }
func (ModelRequestCompleted) isPayload() {}

type ModelRequestFailed struct {
	Info       ModelRequestInfo
	StopReason string
	Error      string
	Partial    string
	Usage      *modelapi.Usage
}

func (ModelRequestFailed) Kind() Kind { return KindModelRequestFailed }
func (ModelRequestFailed) isPayload() {}

type ModelRequestInterrupted ModelRequestFailed

func (ModelRequestInterrupted) Kind() Kind { return KindModelRequestInterrupted }
func (ModelRequestInterrupted) isPayload() {}

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
	Arguments string
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
	CallID      string
	Name        string
	Arguments   string
	RuleID      string
	Action      security.ApprovalMode
	RiskVerdict security.RiskVerdict
	RiskReason  string
}

func (ToolApprovalRequired) Kind() Kind { return KindToolApprovalRequired }
func (ToolApprovalRequired) isPayload() {}

type ToolApprovalResolved struct {
	CallID string
}

func (ToolApprovalResolved) Kind() Kind { return KindToolApprovalResolved }
func (ToolApprovalResolved) isPayload() {}

type ToolResult struct {
	CallID  string
	Result  string
	IsError bool
	Timing  *timing.Record
}

func (ToolResult) Kind() Kind { return KindToolResult }
func (ToolResult) isPayload() {}

type TurnCompleted struct{}

func (TurnCompleted) Kind() Kind { return KindTurnCompleted }
func (TurnCompleted) isPayload() {}

type RunStarted struct {
	StartedAt time.Time
}

func (RunStarted) Kind() Kind { return KindRunStarted }
func (RunStarted) isPayload() {}

type RunEnding struct{}

func (RunEnding) Kind() Kind { return KindRunEnding }
func (RunEnding) isPayload() {}

type RunEnded struct {
	StartedAt      time.Time
	EndedAt        time.Time
	Outcome        sessionentry.RunOutcome
	Iterations     uint64
	ToolCalls      int
	FailureCode    sessionentry.RunFailureCode
	FailureMessage string
	State          agentmodel.AgentState
}

func (RunEnded) Kind() Kind { return KindRunEnded }
func (RunEnded) isPayload() {}

type SubagentStarted struct {
	Name          string
	CreatedAt     time.Time
	Entries       []*sessionentry.Entry
	ParentAgentID identity.AgentID
	ParentRunID   identity.RunID
	CallID        string
}

func (SubagentStarted) Kind() Kind { return KindSubagentStarted }
func (SubagentStarted) isPayload() {}

type SubagentReleased struct{}

func (SubagentReleased) Kind() Kind { return KindSubagentReleased }
func (SubagentReleased) isPayload() {}

type TaskQueueChanged struct {
	Tasks  []taskmodel.Task
	Paused bool
}

func (TaskQueueChanged) Kind() Kind { return KindTaskQueueChanged }
func (TaskQueueChanged) isPayload() {}

type TaskQueueStorageFailed struct {
	Message string
}

func (TaskQueueStorageFailed) Kind() Kind { return KindTaskQueueStorageFailed }
func (TaskQueueStorageFailed) isPayload() {}

type ErrorOccurred struct {
	Message string
}

func (ErrorOccurred) Kind() Kind { return KindError }
func (ErrorOccurred) isPayload() {}

type TurnFailed struct{}

func (TurnFailed) Kind() Kind { return KindTurnFailed }
func (TurnFailed) isPayload() {}

type TurnInterrupted struct{}

func (TurnInterrupted) Kind() Kind { return KindTurnInterrupted }
func (TurnInterrupted) isPayload() {}

type AgentStateChanged struct{ State agentmodel.AgentState }

func (AgentStateChanged) Kind() Kind { return KindAgentStateChanged }
func (AgentStateChanged) isPayload() {}
