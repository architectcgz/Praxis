package bindings

import "time"

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

// AgentSessionConfigReloaded 在会话重载成功后直接推送最新模型目录与有效授权模式。
type AgentSessionConfigReloaded struct {
	Models       []ModelOption       `json:"models"`
	ApprovalMode SessionApprovalMode `json:"approvalMode"`
}

// AgentSessionTitleChanged 在首条输入自动命名后直接推送标题与更新时间。
type AgentSessionTitleChanged struct {
	Title     string    `json:"title"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AgentToolApprovalResolved 通知前端移除已完成或被取消的待审批调用。
type AgentToolApprovalResolved struct {
	CallID string `json:"callId"`
}

type AgentToolResult struct {
	CallID  string  `json:"callId"`
	Result  string  `json:"result"`
	IsError bool    `json:"isError"`
	Timing  *Timing `json:"timing,omitempty"`
}

type AgentRunStarted struct {
	StartedAt time.Time `json:"startedAt"`
}

type AgentRunEnded struct {
	StartedAt      time.Time `json:"startedAt,omitzero"`
	EndedAt        time.Time `json:"endedAt"`
	State          string    `json:"state"`
	Outcome        string    `json:"outcome"`
	Iterations     uint64    `json:"iterations"`
	ToolCalls      int       `json:"toolCalls"`
	FailureCode    string    `json:"failureCode,omitempty"`
	FailureMessage string    `json:"failureMessage,omitempty"`
}

type AgentSubagentStarted struct {
	Name          string           `json:"name"`
	CreatedAt     time.Time        `json:"createdAt"`
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

type AgentStateChanged struct {
	State string `json:"state"`
}
