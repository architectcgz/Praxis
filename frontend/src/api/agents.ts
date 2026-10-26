import type { AgentView as GeneratedAgentView } from '../../bindings/praxis/internal/service/agent/models'
import type { Entry as GeneratedEntry, EntryBlock } from '../../bindings/praxis/wails/dto/models'
import { Events } from '@wailsio/runtime'
import { getAgentBinding, isBindingAvailable } from './bindings'
import { normalizeOperationTiming, type OperationTiming } from './timings'

export const AGENT_EVENT_NAME = 'praxis:agent-event'

export type ExecutionOutcome = '' | 'completed' | 'yielded' | 'paused' | 'failed' | 'interrupted' | 'unknown'
export type ExecutionFailureCode =
    | ''
    | 'provider_unavailable'
    | 'execution_provider_error'
    | 'execution_tool_error'
    | 'execution_policy_blocked'
    | 'execution_approval_required'
    | 'execution_resource_limit'
    | 'execution_storage_error'
    | 'execution_contract_error'
    | 'execution_busy'
    | 'execution_interrupted'
    | 'request_canceled'
    | 'execution_cancelled'
    | 'execution_failed'
    | 'execution_invalid_outcome'
    | 'unknown'
export type Entry = Omit<GeneratedEntry, 'content'> & { content: EntryBlock[] }
export type { EntryBlock }

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
    agentId: string
    executionId: string
    turnId: string
    usage: ModelUsage
}

type AgentEventData = {
    entries_appended: { entries: Entry[]; transient: boolean }
    turn_started: Record<string, never>
    provider_waiting: Record<string, never>
    text_delta: { text: string }
    thinking_delta: { text: string }
    tool_call: { callId: string; name: string; input: unknown }
    tool_approval_required: { callId: string; name: string; input: unknown }
    tool_result: { callId: string; result: string; isError: boolean }
    turn_completed: { stopReason: string }
    model_usage: { usage: ModelUsage }
    request_canceled: Record<string, never>
    execution_ended: { outcome: ExecutionOutcome; failureCode: ExecutionFailureCode; failureMessage?: string }
    error: { message: string }
    operation_timing: { timing: OperationTiming }
}

type AgentEventScope = {
    sessionId?: string
    agentId: string
    executionId: string
    turnId?: string
}

/** kind 与 data 共同构成判别联合，消费端缩窄事件种类后才能读取对应载荷。 */
export type AgentEvent = {
    [K in keyof AgentEventData]: AgentEventScope & { kind: K; data: AgentEventData[K] }
}[keyof AgentEventData]

export function getAgent(agentID: string) {
    return getAgentBinding().GetAgent(agentID)
}

export async function listAgentEntries(agentID: string): Promise<Entry[]> {
    return (await getAgentBinding().ListAgentEntries(agentID) ?? []).map(normalizeEntry)
}

export async function listSessionUsage(sessionId: string): Promise<ModelUsageRecord[]> {
    const values: unknown = await getAgentBinding().ListSessionUsage(sessionId)
    return normalizeModelUsageRecords(values, sessionId)
}

/** 校验持久化请求用量的会话归属和可选计数，供明细与汇总查询共用。 */
export function normalizeModelUsageRecords(values: unknown, sessionId: string): ModelUsageRecord[] {
    return requiredArray(values).map((value) => {
        const record = requiredRecord(value)
        const usage = normalizeModelUsage(record.usage)
        if (record.sessionId !== sessionId || !usage) {
            throw new Error('Token 用量数据格式无效。')
        }
        const agentId = requiredString(record.agentId)
        const executionId = requiredString(record.executionId)
        const turnId = requiredString(record.turnId)
        if (!agentId || !executionId || !turnId) throw new Error('Token 用量数据格式无效。')
        return { sessionId, agentId, executionId, turnId, usage }
    })
}

export function subscribeAgentEvents(listener: (event: AgentEvent) => void) {
    if (!isBindingAvailable()) {
        return () => { }
    }
    return Events.On(AGENT_EVENT_NAME, (notification) => {
        const event = parseAgentEvent(notification.data)
        if (event) {
            listener(event)
        }
    })
}

function parseAgentEvent(value: unknown): AgentEvent | null {
    if (!isRecord(value) || typeof value.agentId !== 'string' || !value.agentId ||
        typeof value.executionId !== 'string' || !value.executionId || !isRecord(value.data)) return null
    const scope: AgentEventScope = {
        sessionId: typeof value.sessionId === 'string' ? value.sessionId : undefined,
        agentId: value.agentId,
        executionId: value.executionId,
        turnId: typeof value.turnId === 'string' && value.turnId ? value.turnId : undefined,
    }
    const data = value.data
    switch (value.kind) {
        case 'entries_appended': {
            if (typeof data.transient !== 'boolean' || data.entries !== null &&
                (!Array.isArray(data.entries) || !data.entries.every(isRecord))) return null
            return { ...scope, kind: value.kind, data: {
                entries: ((data.entries ?? []) as unknown as GeneratedEntry[]).map(normalizeEntry),
                transient: data.transient,
            } }
        }
        case 'turn_started':
        case 'provider_waiting':
            return { ...scope, kind: value.kind, data: {} }
        case 'request_canceled':
            if (!scope.sessionId) return null
            return { ...scope, kind: value.kind, data: {} }
        case 'text_delta':
        case 'thinking_delta':
            if (typeof data.text !== 'string') return null
            return { ...scope, kind: value.kind, data: { text: data.text } }
        case 'tool_call':
        case 'tool_approval_required':
            if (typeof data.callId !== 'string' || !data.callId || typeof data.name !== 'string' || !('input' in data)) return null
            return { ...scope, kind: value.kind, data: { callId: data.callId, name: data.name, input: data.input } }
        case 'tool_result':
            if (typeof data.callId !== 'string' || !data.callId || typeof data.result !== 'string' || typeof data.isError !== 'boolean') return null
            return { ...scope, kind: value.kind, data: { callId: data.callId, result: data.result, isError: data.isError } }
        case 'turn_completed':
            if (typeof data.stopReason !== 'string') return null
            return { ...scope, kind: value.kind, data: { stopReason: data.stopReason } }
        case 'model_usage': {
            const usage = normalizeModelUsage(data.usage)
            if (!usage || !scope.sessionId || !scope.turnId) return null
            return { ...scope, kind: value.kind, data: { usage } }
        }
        case 'execution_ended': {
            const outcome = enumValue(data.outcome, executionOutcomes, 'unknown')
            const failureCode = failureCodeValue(data.failureCode)
            if (!scope.sessionId || outcome === '' || outcome === 'unknown' || failureCode === 'unknown') return null
            return { ...scope, kind: value.kind, data: {
                outcome,
                failureCode,
                failureMessage: typeof data.failureMessage === 'string' ? data.failureMessage : undefined,
            } }
        }
        case 'operation_timing': {
            try {
                const timing = normalizeOperationTiming(data.timing)
                if (timing.agentId !== scope.agentId || timing.executionId !== scope.executionId) return null
                return { ...scope, kind: value.kind, data: { timing } }
            } catch {
                return null
            }
        }
        case 'error':
            if (typeof data.message !== 'string') return null
            return { ...scope, kind: value.kind, data: { message: data.message } }
        default:
            return null
    }
}

// Go 的空切片可能编码为 null；只在 API 边界统一为可遍历的内容集合。
function normalizeEntry(value: GeneratedEntry): Entry {
    return { ...value, content: value.content ?? [] }
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null
}

function normalizeModelUsage(value: unknown): ModelUsage | null {
    if (!isRecord(value) || !Number.isSafeInteger(value.inputTokens) || Number(value.inputTokens) < 0) return null
    const output = value.outputTokens
    const read = value.cacheReadInputTokens
    const creation = value.cacheCreationInputTokens
    if (output != null && (!Number.isSafeInteger(output) || Number(output) < 0 || !Number.isSafeInteger(Number(value.inputTokens) + Number(output))) ||
        read != null && (!Number.isSafeInteger(read) || Number(read) < 0) ||
        creation != null && (!Number.isSafeInteger(creation) || Number(creation) < 0) ||
        Number(read ?? 0) + Number(creation ?? 0) > Number(value.inputTokens)) return null
    return {
        inputTokens: value.inputTokens as number,
        outputTokens: output == null ? undefined : output as number,
        cacheReadInputTokens: read == null ? undefined : read as number,
        cacheCreationInputTokens: creation == null ? undefined : creation as number,
    }
}

const executionOutcomes = ['', 'completed', 'yielded', 'paused', 'failed', 'interrupted'] as const
const executionFailureCodes = [
    '',
    'provider_unavailable',
    'execution_provider_error',
    'execution_tool_error',
    'execution_policy_blocked',
    'execution_approval_required',
    'execution_resource_limit',
    'execution_storage_error',
    'execution_contract_error',
    'execution_busy',
    'execution_interrupted',
    'request_canceled',
    'execution_cancelled',
    'execution_failed',
    'execution_invalid_outcome',
] as const

function failureCodeValue(value: unknown): ExecutionFailureCode {
    if (value == null) {
        return ''
    }
    return enumValue(value, executionFailureCodes, 'unknown')
}

function enumValue<T extends string>(value: unknown, values: readonly T[], fallback: T): T {
    return typeof value === 'string' && values.includes(value as T) ? value as T : fallback
}

function requiredRecord(value: unknown): Record<string, unknown> {
    if (!isRecord(value)) {
        throw new Error('Agent 数据格式无效。')
    }
    return value
}

function requiredArray(value: unknown): unknown[] {
    if (!Array.isArray(value)) {
        throw new Error('Agent 数据格式无效。')
    }
    return value
}

function requiredString(value: unknown): string {
    if (typeof value !== 'string') {
        throw new Error('Agent 数据格式无效。')
    }
    return value
}
