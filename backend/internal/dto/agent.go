package dto

import (
	"time"
)

type StopRunRequest struct {
	SessionID string `json:"sessionId" validate:"session_id"`
	RunID     string `json:"runId" validate:"id"`
}

// AgentEvent 是展示流的传输信封；Data 由绑定层按核心载荷显式映射，不直接序列化核心接口。
type AgentEvent struct {
	Kind    string `json:"kind"`
	AgentID string `json:"agentId,omitempty"`
	RunID   string `json:"runId,omitempty"`
	TurnID  string `json:"turnId,omitempty"`
	Data    any    `json:"data"`
}

type AgentMessagesAppended struct {
	Messages []HistoryMessage `json:"messages"`
}

type AgentTextDelta struct {
	Text string `json:"text"`
}

type AgentThinkingDelta struct {
	Text string `json:"text"`
}

// AgentToolCall 的 Name 是工具注册键，Arguments 保留模型返回的参数原文。
type AgentToolCall struct {
	CallID    string `json:"callId"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

// AgentToolApprovalRequired 通过信封中的 runId 与 callId 关联调用，审批详情由查询入口返回。
type AgentToolApprovalRequired struct {
	CallID string `json:"callId"`
}

type AgentToolResult struct {
	CallID  string  `json:"callId"`
	Result  string  `json:"result"`
	IsError bool    `json:"isError"`
	Timing  *Timing `json:"timing,omitempty"`
}

type AgentTurnCompleted struct {
	StopReason string `json:"stopReason"`
}

type AgentRunChanged struct {
	Run RunSummary `json:"run"`
}

type AgentRunEnded struct {
	Run      RunSummary       `json:"run"`
	State    string           `json:"state"`
	Messages []HistoryMessage `json:"messages"`
}

type AgentSubagentStarted struct {
	Agent         AgentView        `json:"agent"`
	Messages      []HistoryMessage `json:"messages"`
	ParentAgentID string           `json:"parentAgentId"`
	ParentRunID   string           `json:"parentRunId"`
	CallID        string           `json:"callId"`
}

type AgentTaskQueueChanged struct {
	Tasks  []QueuedTask `json:"tasks"`
	Paused bool         `json:"paused"`
}

type AgentToolStarted struct {
	CallID string  `json:"callId"`
	Timing *Timing `json:"timing,omitempty"`
}

type AgentErrorOccurred struct {
	Message string `json:"message"`
}

// RunSummary 是外部展示摘要，不携带执行配置、句柄或推理续传状态。
type RunSummary struct {
	ID          string
	Status      string
	StartedAt   time.Time
	Iterations  uint64
	ToolCalls   int
	OutputBytes int64
	Result      *RunResult `json:"Result,omitempty"`
}

// RunResult 只在执行结束后出现，失败原因归属于结果而非运行阶段。
type RunResult struct {
	Outcome        string
	EndedAt        time.Time
	FailureCode    string
	FailureMessage string
}
type AgentView struct {
	ID           string
	SessionID    string
	DefinitionID string
	Kind         string
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Name         string      `json:"name"`
	State        string      `json:"state"`
	CurrentRunID string      `json:"currentRunId"`
	CurrentRun   *RunSummary `json:"currentRun"`
	LatestRun    *RunSummary `json:"latestRun"`
}
type AgentStateChanged struct {
	State string `json:"state"`
}
