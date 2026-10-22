package loop

import (
	"encoding/json"
	modelapi "praxis/internal/core/model"
	sessionentry "praxis/internal/core/session/entry"
	"praxis/internal/identity"
)

// EventKind 标识可推送给观察者的瞬时 Loop 事件。
type EventKind string

const (
	EventEntriesAppended EventKind = "entries_appended"
	EventTurnStarted     EventKind = "turn_started"
	EventProviderWaiting EventKind = "provider_waiting"
	EventTextDelta       EventKind = "text_delta"
	EventThinkingDelta   EventKind = "thinking_delta"
	EventToolCall        EventKind = "tool_call"
	EventToolResult      EventKind = "tool_result"
	EventTurnCompleted   EventKind = "turn_completed"
	EventModelUsage      EventKind = "model_usage"
	EventError           EventKind = "error"
)

// Event 是单次 执行输入 的瞬时 Loop 事件；持久化消息与执行状态是最终事实。
// 前端断线后应重新读取对应消息流和执行状态恢复视图。
type Event struct {
	Entries     []sessionentry.ContextEntry `json:"entries,omitempty"`
	Kind        EventKind                   `json:"kind"`
	SessionID   identity.SessionID          `json:"sessionId,omitempty"`
	AgentID     identity.AgentID            `json:"agentId"`
	ExecutionID identity.AgentExecutionID   `json:"executionId"`
	TurnID      identity.TurnID             `json:"turnId,omitempty"`
	StopReason  modelapi.ModelStopReason    `json:"stopReason,omitempty"`
	Text        string                      `json:"text,omitempty"`
	CallID      string                      `json:"callId,omitempty"`
	Name        string                      `json:"name,omitempty"`
	Input       json.RawMessage             `json:"input,omitempty"`
	Result      string                      `json:"result,omitempty"`
	IsError     bool                        `json:"isError,omitzero"`
	Error       string                      `json:"error,omitempty"`
	Usage       *modelapi.ModelUsage        `json:"usage,omitempty"`
}

// EventObserver 接收实时事件；监听者不得把事件作为持久状态来源。
type EventObserver func(Event)
