package dto

import (
	"encoding/json"
	agentmodel "praxis/internal/core/agent"
	agentexecution "praxis/internal/core/agent/execution"
	agentservice "praxis/internal/service/agent"
	"time"
)

type PauseAgentRequest struct {
	AgentID           string `json:"agentId" validate:"id"`
	TargetExecutionID string `json:"targetExecutionId" validate:"id"`
}

type StopAgentRequest = PauseAgentRequest

// AgentControlResponse 的 applied 表示信号已接受，不代表执行槽位已经释放。
type AgentControlResponse struct {
	AgentID           string `json:"agentId"`
	TargetExecutionID string `json:"targetExecutionId"`
	Applied           bool   `json:"applied"`
}

// AgentEvent 是展示流的传输信封；Data 由绑定层按核心载荷显式映射，不直接序列化核心接口。
type AgentEvent struct {
	BranchID    string `json:"branchId"`
	BindingID   string `json:"bindingId"`
	Kind        string `json:"kind"`
	SessionID   string `json:"sessionId,omitempty"`
	ExecutionID string `json:"executionId"`
	TurnID      string `json:"turnId,omitempty"`
	Data        any    `json:"data"`
}

type AgentEntriesAppended struct {
	Entries   []Entry `json:"entries"`
	Transient bool    `json:"transient"`
}

type AgentTextDelta struct {
	Text string `json:"text"`
}

type AgentThinkingDelta struct {
	Text string `json:"text"`
}

type AgentToolCall struct {
	CallID string          `json:"callId"`
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
}

type AgentToolApprovalRequired struct {
	CallID string          `json:"callId"`
	Name   string          `json:"name"`
	Input  json.RawMessage `json:"input"`
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
