import type { RunRecord } from '../../bindings/praxis/internal/core/agent/run/models'
import type { QueuedTask } from './tasks'
import type { AgentView as GeneratedAgentView } from '../../bindings/praxis/internal/service/agent/models'
import type { Entry as GeneratedEntry, EntryBlock } from '../../bindings/praxis/wails/dto/models'
import { Events } from '@wailsio/runtime'
import { getAgentBinding, isBindingAvailable } from './bindings'
import type { Timing } from './timings'

export const AGENT_EVENT_NAME = 'praxis:agent-event'

// Go 的展示投影保证 content 为数组，缺失的 timing 字段直接省略，不编码为 null。
export type Entry = Omit<GeneratedEntry, 'content' | 'timing'> & { content: EntryBlock[]; timing?: Timing }
export type { EntryBlock, RunRecord }

/** 保留生成接口的字段，将枚举暴露为其字面量联合，允许前端表达乐观状态。 */
export type AgentView = Omit<GeneratedAgentView, 'state'> & { state: `${GeneratedAgentView['state']}` }

export type ModelUsage = {
    inputTokens: number
    outputTokens?: number
    cacheReadInputTokens?: number
    cacheCreationInputTokens?: number
}

export type ModelUsageRecord = {
    sessionId: string
    runId: string
    turnId: string
    usage: ModelUsage
}

type AgentEventData = {
    entries_appended: { entries: Entry[] }
    turn_started: Record<string, never>
    model_request_started: { requestKind: 'iteration' | 'summary' | 'risk_review'; timing?: Timing }
    model_request_finished: { requestKind: 'iteration' | 'summary' | 'risk_review'; stopReason: string; usage?: ModelUsage; timing?: Timing }
    text_delta: { text: string }
    thinking_delta: { text: string }
    tool_call: { callId: string; name: string; arguments: unknown }
    tool_started: { callId: string; timing?: Timing }
    tool_approval_required: { callId: string }
    tool_result: { callId: string; result: string; isError: boolean; timing?: Timing }
    turn_completed: { stopReason: string }
    run_started: { run: RunRecord }
    run_interrupt_requested: { appliedAt: string }
    request_canceled: { run: RunRecord; state: AgentView['state']; entries: Entry[] }
    run_ended: { run: RunRecord; state: AgentView['state']; entries: Entry[] }
    subagent_started: { agent: AgentView; entries: Entry[]; parentAgentId: string; parentRunId: string; callId: string }
    subagent_released: Record<string, never>
    task_queue_changed: { tasks: QueuedTask[] }
    error: { message: string }
}

type AgentEventSource = {
    agentId: string
    bindingId: string
    branchId: string
    sessionId?: string
    runId: string
    turnId?: string
}

/** kind 与 data 共同构成判别联合，消费端缩窄事件种类后才能读取对应载荷。 */
export type AgentEvent = {
    [K in keyof AgentEventData]: AgentEventSource & { kind: K; data: AgentEventData[K] }
}[keyof AgentEventData]

export function getAgent(sessionID: string, branchID: string, agentID: string) {
    return getAgentBinding().GetAgent(sessionID, branchID, agentID)
}

export async function listAgentEntries(sessionID: string, branchID: string, agentID: string): Promise<Entry[]> {
    return (await getAgentBinding().ListAgentEntries(sessionID, branchID, agentID) ?? []) as Entry[]
}

export async function listSessionUsage(sessionId: string): Promise<ModelUsageRecord[]> {
    return (await getAgentBinding().ListSessionUsage(sessionId) ?? []) as ModelUsageRecord[]
}

export function subscribeAgentEvents(listener: (event: AgentEvent) => void) {
    if (!isBindingAvailable()) return () => { }
    return Events.On(AGENT_EVENT_NAME, (notification) => listener(notification.data as AgentEvent))
}
