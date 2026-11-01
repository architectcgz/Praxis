package dto

import (
	"encoding/json"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	agentservice "praxis/internal/service/agent"
	"time"
)

type StopAgentRequest struct {
	AgentID string `json:"agentId" validate:"id"`
}

// AgentEvent 是展示流的传输信封；Data 由绑定层按核心载荷显式映射，不直接序列化核心接口。
type AgentEvent struct {
	BranchID    string `json:"branchId"`
	BindingID   string `json:"bindingId"`
	Kind        string `json:"kind"`
	SessionID   string `json:"sessionId,omitempty"`
	AgentID     string `json:"agentId"`
	ExecutionID string `json:"executionId"`
	TurnID      string `json:"turnId,omitempty"`
	Data        any    `json:"data"`
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

// AgentToolCall 的 Name 是工具注册键，Arguments 仅包含该工具定义的入参。
type AgentToolCall struct {
	CallID    string          `json:"callId"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// AgentToolApprovalRequired 通过信封中的 executionId 与 callId 关联调用，审批详情由查询入口返回。
type AgentToolApprovalRequired struct {
	CallID string `json:"callId"`
}

type AgentToolResult struct {
	CallID  string `json:"callId"`
	Result  string `json:"result"`
	IsError bool   `json:"isError"`
}

type AgentTurnCompleted struct {
	StopReason string `json:"stopReason"`
}

type AgentModelUsage struct {
	Usage ModelUsage `json:"usage"`
}

type AgentExecutionStarted struct {
	Execution agentexecution.ExecutionRecord `json:"execution"`
}

type AgentExecutionControl struct {
	AppliedAt time.Time `json:"appliedAt"`
}

type AgentExecutionEnded struct {
	Execution agentexecution.ExecutionRecord `json:"execution"`
	State     agentmodel.AgentState          `json:"state"`
	Entries   []Entry                        `json:"entries"`
}

type AgentSubagentStarted struct {
	Agent             agentservice.AgentView `json:"agent"`
	Entries           []Entry                `json:"entries"`
	ParentAgentID     string                 `json:"parentAgentId"`
	ParentExecutionID string                 `json:"parentExecutionId"`
	CallID            string                 `json:"callId"`
}

type AgentTaskQueueChanged struct {
	Tasks []QueuedTask `json:"tasks"`
}

type AgentOperationTiming struct {
	Timing OperationTiming `json:"timing"`
}

type AgentErrorOccurred struct {
	Message string `json:"message"`
}
