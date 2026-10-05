package agentruntime

import (
	"praxis/internal/contracts"
	"praxis/internal/timing"

	"encoding/json"
)

// AgentEventKind 标识可推送给观察者的瞬时 Agent runtime 事件。
type AgentEventKind string

const (
	AgentEventStepStarted     AgentEventKind = "step_started"
	AgentEventProviderWaiting AgentEventKind = "provider_waiting"
	AgentEventTextDelta       AgentEventKind = "text_delta"
	AgentEventThinkingDelta   AgentEventKind = "thinking_delta"
	AgentEventToolCall        AgentEventKind = "tool_call"
	AgentEventToolResult      AgentEventKind = "tool_result"
	AgentEventStepCompleted   AgentEventKind = "step_completed"
	AgentEventModelUsage      AgentEventKind = "model_usage"
	AgentEventTurnSettled     AgentEventKind = "settled"
	AgentEventError           AgentEventKind = "error"
	AgentEventTiming          AgentEventKind = "operation_timing"
)

// AgentEvent 是单次 turn 的瞬时 runtime 事件；持久化消息与执行状态是最终事实。
// 前端断线后应重新读取对应消息流和执行状态恢复视图。
type AgentEvent struct {
	Kind      AgentEventKind      `json:"kind"`
	SessionID contracts.SessionID `json:"sessionId,omitempty"`
	AgentID   contracts.AgentID   `json:"agentId"`
	TurnID    contracts.TurnID    `json:"turnId"`
	Step      int                 `json:"step,omitzero"`
	Text      string              `json:"text,omitempty"`
	CallID    string              `json:"callId,omitempty"`
	Name      string              `json:"name,omitempty"`
	Input     json.RawMessage     `json:"input,omitempty"`
	Result    string              `json:"result,omitempty"`
	IsError   bool                `json:"isError,omitzero"`
	Error     string              `json:"error,omitempty"`
	Timing    *timing.Record      `json:"timing,omitempty"`
	Usage     *ModelUsage         `json:"usage,omitempty"`
}

// AgentEventObserver 接收实时事件；监听者不得把事件作为持久状态来源。
type AgentEventObserver func(AgentEvent)
