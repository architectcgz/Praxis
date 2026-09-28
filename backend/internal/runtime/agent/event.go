package agent

import (
	"praxis/internal/contracts"

	"encoding/json"
)

// AgentEventKind 标识可推送给观察者的瞬时 Agent runtime 事件。
type AgentEventKind string

const (
	AgentEventTurnStarted      AgentEventKind = "turn_started"
	AgentEventTextDelta        AgentEventKind = "text_delta"
	AgentEventThinkingDelta    AgentEventKind = "thinking_delta"
	AgentEventToolCall         AgentEventKind = "tool_call"
	AgentEventToolResult       AgentEventKind = "tool_result"
	AgentEventTurnCompleted    AgentEventKind = "turn_completed"
	AgentEventExecutionSettled AgentEventKind = "settled"
	AgentEventError            AgentEventKind = "error"
)

// AgentEvent 是单次 execution 的瞬时 runtime 事件；持久化结果以 transcript 为准。
// 不同事件只使用与自身相关的字段，前端断线后必须重新读取 transcript 恢复状态。
type AgentEvent struct {
	Kind        AgentEventKind             `json:"kind"`
	AgentID     contracts.AgentID          `json:"agentId"`
	ExecutionID contracts.AgentExecutionID `json:"executionId"`
	Turn        int                        `json:"turn,omitzero"`
	Text        string                     `json:"text,omitempty"`
	CallID      string                     `json:"callId,omitempty"`
	Name        string                     `json:"name,omitempty"`
	Input       json.RawMessage            `json:"input,omitempty"`
	Result      string                     `json:"result,omitempty"`
	IsError     bool                       `json:"isError,omitzero"`
	Error       string                     `json:"error,omitempty"`
}

// AgentEventObserver 接收实时事件；监听者不得把事件作为持久状态来源。
type AgentEventObserver func(AgentEvent)
