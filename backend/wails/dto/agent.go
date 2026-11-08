package dto

import (
	agentmodel "praxis/internal/core/agent"
	agentrun "praxis/internal/core/agent/run"
	"praxis/internal/core/agent/run/timing"
	agentservice "praxis/internal/service/agent"
	"time"
)

type StopAgentRequest struct {
	AgentID string `json:"agentId" validate:"id"`
}

// AgentEvent 是展示流的传输信封；Data 由绑定层按核心载荷显式映射，不直接序列化核心接口。
type AgentEvent struct {
	BranchID  string `json:"branchId"`
	BindingID string `json:"bindingId"`
	Kind      string `json:"kind"`
	SessionID string `json:"sessionId,omitempty"`
	AgentID   string `json:"agentId"`
	RunID     string `json:"runId"`
	TurnID    string `json:"turnId,omitempty"`
	Data      any    `json:"data"`
}

type AgentEntriesAppended struct {
	Entries []Entry `json:"entries"`
}

type AgentTextDelta struct {
	Text string `json:"text"`
}

type AgentThinkingDelta struct {
	Text string `json:"text"`
}

type AgentModelRequestStarted struct {
	RequestKind string         `json:"requestKind"`
	Timing      *timing.Record `json:"timing,omitempty"`
}

type AgentModelRequestFinished struct {
	RequestKind string         `json:"requestKind"`
	StopReason  string         `json:"stopReason"`
	Usage       *ModelUsage    `json:"usage,omitempty"`
	Timing      *timing.Record `json:"timing,omitempty"`
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
	CallID  string         `json:"callId"`
	Result  string         `json:"result"`
	IsError bool           `json:"isError"`
	Timing  *timing.Record `json:"timing,omitempty"`
}

type AgentTurnCompleted struct {
	StopReason string `json:"stopReason"`
}

type AgentRunStarted struct {
	Run agentrun.RunRecord `json:"run"`
}

type AgentRunControl struct {
	AppliedAt time.Time `json:"appliedAt"`
}

type AgentRunEnded struct {
	Run     agentrun.RunRecord    `json:"run"`
	State   agentmodel.AgentState `json:"state"`
	Entries []Entry               `json:"entries"`
}

type AgentSubagentStarted struct {
	Agent         agentservice.AgentView `json:"agent"`
	Entries       []Entry                `json:"entries"`
	ParentAgentID string                 `json:"parentAgentId"`
	ParentRunID   string                 `json:"parentRunId"`
	CallID        string                 `json:"callId"`
}

type AgentTaskQueueChanged struct {
	Tasks []QueuedTask `json:"tasks"`
}

type AgentToolStarted struct {
	CallID string         `json:"callId"`
	Timing *timing.Record `json:"timing,omitempty"`
}

type AgentErrorOccurred struct {
	Message string `json:"message"`
}
