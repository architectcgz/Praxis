import { Events } from '@wailsio/runtime'
import type { Timing } from '../../bindings/praxis/wails/bindings'
import { isBindingAvailable } from './bindings'
import type { AgentView, HistoryMessage, ModelRequestActivity, RunOutcome } from './agents'
import type { QueuedTask } from './tasks'

type RunData = {
    entries_appended: { messages: HistoryMessage[] }
    run_started: { startedAt: string }
    run_ending: Record<string, never>
    agent_state_changed: { state: AgentView['state'] }
    run_ended: {
        startedAt?: string
        endedAt: string
        state: AgentView['state']
        outcome: RunOutcome
        iterations: number
        toolCalls: number
        failureCode?: string
        failureMessage?: string
    }
    subagent_started: { name: string; createdAt: string; messages: HistoryMessage[]; parentAgentId: string; parentRunId: string; callId: string }
    subagent_released: Record<string, never>
    error: { message: string }
}
type TurnData = {
    turn_started: Record<string, never>
    turn_completed: Record<string, never>
    turn_failed: Record<string, never>
    turn_interrupted: Record<string, never>
    text_delta: { text: string }
    thinking_delta: { text: string }
    tool_call: { callId: string; name: string; arguments: unknown }
    tool_started: { callId: string; timing?: Timing }
    tool_approval_required: { callId: string }
    tool_result: { callId: string; result: string; isError: boolean; timing?: Timing }
}
type RunSource = { agentId: string; runId: string }
type RequestEventKind = 'model_request_started' | 'model_request_completed' | 'model_request_failed' | 'model_request_interrupted'
export type RequestEvent = {
    [K in RequestEventKind]: RunSource & { kind: K; turnId?: string; data: ModelRequestActivity }
}[RequestEventKind]

export function isRequestEvent(event: AgentEvent): event is RequestEvent {
    switch (event.kind) {
        case 'model_request_started':
        case 'model_request_completed':
        case 'model_request_failed':
        case 'model_request_interrupted':
            return true
        default:
            return false
    }
}

/** Session 身份已绑定到通道，运行事件按 Agent 身份路由。 */
export type AgentEvent =
    | { [K in keyof RunData]: RunSource & { kind: K; data: RunData[K] } }[keyof RunData]
    | { [K in keyof TurnData]: RunSource & { kind: K; turnId: string; data: TurnData[K] } }[keyof TurnData]
    | RequestEvent
    | { kind: 'task_queue_changed'; data: { tasks: QueuedTask[]; paused: boolean } }
    | { kind: 'task_queue_storage_failed'; data: { message: string } }
    | { kind: 'session_config_reloaded'; data: Record<string, never> }
    | { kind: 'session_title_changed'; data: Record<string, never> }

export function subscribeAgentEvents(sessionId: string, listener: (event: AgentEvent) => void) {
    if (!isBindingAvailable()) return () => { }
    return Events.On(`praxis:session:${sessionId}`, (notification) => listener(notification.data as AgentEvent))
}
