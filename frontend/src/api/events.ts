import { Events } from '@wailsio/runtime'
import { isBindingAvailable } from './bindings'
import type { AgentView, Entry, RunSnapshot, ModelUsage } from './agents'
import type { Timing } from './timings'
import type { QueuedTask } from './tasks'

type RunData = {
    entries_appended: { agentId: string; entries: Entry[] }
    run_started: { agentId: string; run: RunSnapshot }
    run_interrupt_requested: { appliedAt: string }
    agent_state_changed: { state: AgentView['state'] }
    run_ended: { run: RunSnapshot; state: AgentView['state']; entries: Entry[] }
    request_canceled: { run: RunSnapshot; state: AgentView['state']; entries: Entry[] }
    subagent_started: { agent: AgentView; entries: Entry[]; parentAgentId: string; parentRunId: string; callId: string }
    subagent_released: Record<string, never>
    error: { message: string }
}
type TurnData = {
    turn_started: Record<string, never>
    turn_completed: { stopReason: string }
    turn_failed: Record<string, never>
    turn_interrupted: Record<string, never>
    text_delta: { text: string }
    thinking_delta: { text: string }
    tool_call: { callId: string; name: string; arguments: unknown }
    tool_started: { callId: string; timing?: Timing }
    tool_approval_required: { callId: string }
    tool_result: { callId: string; result: string; isError: boolean; timing?: Timing }
}
type RunSource = { runId: string }
type RequestCommon = { requestId: string; retryCount: number; timing?: Timing }
type RequestResults = {
    model_request_started: RequestCommon
    model_request_completed: RequestCommon & { status: 'completed'; stopReason: string; usage?: ModelUsage }
    model_request_failed: RequestCommon & { status: 'failed'; stopReason: string; error: string; usage?: ModelUsage }
    model_request_interrupted: RequestCommon & { status: 'interrupted'; stopReason: string; error: string; usage?: ModelUsage }
}
type RequestEvent = { [K in keyof RequestResults]:
    | (RunSource & { kind: K; turnId: string; data: RequestResults[K] & { requestKind: 'iteration' | 'risk_review' } })
    | (RunSource & { kind: K; turnId?: never; data: RequestResults[K] & { requestKind: 'summary' } })
}[keyof RequestResults]
/** 身份由事件的业务范围决定；Session 身份已经绑定到通道。 */
export type AgentEvent =
    | { [K in keyof RunData]: RunSource & { kind: K; data: RunData[K] } }[keyof RunData]
    | { [K in keyof TurnData]: RunSource & { kind: K; turnId: string; data: TurnData[K] } }[keyof TurnData]
    | RequestEvent
    | { kind: 'task_queue_changed'; data: { tasks: QueuedTask[]; paused: boolean } }
    | { kind: 'session_config_reloaded'; data: Record<string, never> }

export function subscribeAgentEvents(sessionId: string, listener: (event: AgentEvent) => void) {
    if (!isBindingAvailable()) return () => { }
    return Events.On(`praxis:session:${sessionId}`, (notification) => listener(notification.data as AgentEvent))
}
