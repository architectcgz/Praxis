import { EventsOn } from '../../wailsjs/runtime/runtime'
import { getAgentBinding, isBindingAvailable } from './bindings'
import { normalizeOperationTiming, type OperationTiming } from './timings'

export const AGENT_EVENT_NAME = 'praxis:agent-event'

export type AgentState = 'idle' | 'executing' | 'waiting' | 'pausing' | 'paused' | 'interrupted' | 'failed' | 'unknown'
export type ExecutionStatus = 'starting' | 'running' | 'waiting_approval' | 'ending' | 'ended' | 'unknown'
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
export type AgentMessageRole = 'user' | 'assistant' | 'tool' | 'unknown'
export type AgentMessageBlockKind = 'text' | 'thinking' | 'tool_call' | 'tool_result' | 'unknown'
export type AgentHistoryKind = 'message' | 'execution' | 'request_canceled' | 'unknown'

export type AgentSnapshot = {
    id: string
    name: string
    sessionId: string
    definitionId: string
    profile: string
    state: AgentState
    currentExecutionId: string
    executionIds: string[]
    executions: ExecutionSnapshot[]
    tasks: QueuedTaskSnapshot[]
    controlCommandIds: string[]
}

export type QueuedTaskSnapshot = { id: string; status: string; sequence: number; executionId: string }

export type ExecutionSnapshot = {
    sourceTaskId: string
    id: string
    status: ExecutionStatus
    outcome: ExecutionOutcome
    failureCode: ExecutionFailureCode
    failureMessage: string
    createdAt: string
    startedAt: string
    endedAt: string
}

export type AgentMessage = {
    id?: string
    sequence: number
    at: string
    executionId: string
    role: AgentMessageRole
    content: string
    thinking?: string
    blocks?: AgentMessageBlock[]
}

export type AgentMessageBlock = {
    kind: AgentMessageBlockKind
    text?: string
    callId?: string
    name?: string
    input?: unknown
    isError?: boolean
}

export type AgentHistoryItem = {
    kind: AgentHistoryKind
    sequence?: number
    at: string
    message?: AgentMessage
    execution?: ExecutionSnapshot
}

export type AgentHistoryPage = { items: AgentHistoryItem[]; nextCursor: string }

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

export type AgentEvent = {
    kind: 'turn_started' | 'provider_waiting' | 'text_delta' | 'thinking_delta' | 'tool_call' | 'tool_approval_required' | 'tool_result' | 'turn_completed' | 'model_usage' | 'request_canceled' | 'execution_ended' | 'error' | 'operation_timing'
    outcome?: ExecutionOutcome
    failureCode?: ExecutionFailureCode
    failureMessage?: string
    timing?: OperationTiming
    usage?: ModelUsage
    sessionId?: string
    agentId: string
    executionId: string
    turnId?: string
    text?: string
    callId?: string
    name?: string
    input?: unknown
    result?: string
    isError?: boolean
    error?: string
}

export function getAgent(agentID: string) {
    return getAgentBinding().GetAgent(agentID).then(normalizeAgentSnapshot)
}

export function listAgentHistory(agentID: string, before = ''): Promise<AgentHistoryPage> {
    return getAgentBinding().ListAgentHistory(agentID, before).then((value: unknown) => {
        const record = requiredRecord(value)
        return { items: normalizeAgentHistory(record.items), nextCursor: requiredString(record.nextCursor) }
    })
}

/** 查询会话全部请求的用量；缺失计数保留为未知，不推算 token。 */
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

export function normalizeAgentSnapshot(value: unknown): AgentSnapshot {
    const record = requiredRecord(value)
    const definitionID = requiredString(record.definitionId)
    return {
        id: requiredString(record.id),
        name: requiredString(record.name),
        sessionId: requiredString(record.sessionId),
        definitionId: definitionID,
        profile: requiredString(record.profile),
        state: enumValue(record.state, agentStates, 'unknown'),
        currentExecutionId: requiredString(record.currentExecutionId),
        executionIds: optionalStringArray(record.executionIds),
        executions: requiredArray(record.executions).map(normalizeExecutionSnapshot),
        tasks: requiredArray(record.tasks).map((value) => {
            const task = requiredRecord(value)
            return {
                id: requiredString(task.id),
                status: requiredString(task.status),
                sequence: requiredNumber(task.sequence),
                executionId: optionalString(task.executionId),
            }
        }),
        controlCommandIds: optionalStringArray(record.controlCommandIds),
    }
}

export function normalizeAgentHistory(value: unknown): AgentHistoryItem[] {
    return requiredArray(value).map((item) => {
        const record = requiredRecord(item)
        const kind = enumValue(record.kind, historyKinds, 'unknown')
        return {
            kind,
            sequence: kind === 'message' ? requiredNumber(record.sequence) : optionalNumber(record.sequence),
            at: requiredString(record.at),
            message: record.message == null ? undefined : normalizeAgentMessage(record.message),
            execution: record.execution == null ? undefined : normalizeExecutionSnapshot(record.execution),
        }
    })
}

export function subscribeAgentEvents(listener: (event: AgentEvent) => void) {
    if (!isBindingAvailable()) {
        return () => { }
    }
    return EventsOn(AGENT_EVENT_NAME, (value: unknown) => {
        const event = parseAgentEvent(value)
        if (event) {
            listener(event)
        }
    })
}

function parseAgentEvent(value: unknown): AgentEvent | null {
    if (!isRecord(value) || typeof value.agentId !== 'string' ||
        typeof value.executionId !== 'string') {
        return null
    }
    if (value.kind !== 'turn_started' && value.kind !== 'provider_waiting' && value.kind !== 'text_delta' && value.kind !== 'thinking_delta' &&
        value.kind !== 'tool_call' && value.kind !== 'tool_approval_required' && value.kind !== 'tool_result' && value.kind !== 'turn_completed' &&
        value.kind !== 'model_usage' && value.kind !== 'request_canceled' && value.kind !== 'execution_ended' && value.kind !== 'error' && value.kind !== 'operation_timing') {
        return null
    }
    if ((value.kind === 'text_delta' || value.kind === 'thinking_delta') && typeof value.text !== 'string') {
        return null
    }
    if ((value.kind === 'tool_call' || value.kind === 'tool_approval_required' || value.kind === 'tool_result') &&
        (typeof value.callId !== 'string' || typeof value.name !== 'string')) {
        return null
    }
    const terminal = value.kind === 'execution_ended' || value.kind === 'request_canceled'
    const outcome = terminal ? enumValue(value.outcome, executionOutcomes, 'unknown') : undefined
    const failureCode = terminal ? failureCodeValue(value.failureCode) : undefined
    if (terminal && (outcome === '' || outcome === 'unknown' || failureCode === 'unknown' ||
        typeof value.sessionId !== 'string' || !value.sessionId || !value.agentId || !value.executionId)) return null
    if (value.kind === 'request_canceled' && (failureCode !== 'request_canceled' || outcome !== 'paused' && outcome !== 'interrupted')) return null
    const reportedUsage = value.kind === 'model_usage' ? normalizeModelUsage(value.usage) : undefined
    if (value.kind === 'model_usage' && (!reportedUsage || typeof value.turnId !== 'string' || !value.turnId ||
        typeof value.sessionId !== 'string' || !value.sessionId || !value.agentId || !value.executionId)) return null
    let timing: OperationTiming | undefined
    if (value.kind === 'operation_timing') {
        try {
            timing = normalizeOperationTiming(value.timing)
            if (timing.agentId !== value.agentId || timing.executionId !== value.executionId) return null
        } catch {
            return null
        }
    }
    return {
        kind: value.kind,
        outcome,
        failureCode,
        failureMessage: terminal && typeof value.failureMessage === 'string' ? value.failureMessage : undefined,
        timing,
        usage: reportedUsage ?? undefined,
        sessionId: typeof value.sessionId === 'string' ? value.sessionId : undefined,
        agentId: value.agentId,
        executionId: value.executionId,
        turnId: typeof value.turnId === 'string' && value.turnId ? value.turnId : undefined,
        text: typeof value.text === 'string' ? value.text : undefined,
        callId: typeof value.callId === 'string' ? value.callId : undefined,
        name: typeof value.name === 'string' ? value.name : undefined,
        input: value.input,
        result: typeof value.result === 'string' ? value.result : undefined,
        isError: typeof value.isError === 'boolean' ? value.isError : undefined,
        error: typeof value.error === 'string' ? value.error : undefined,
    }
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

const agentStates = ['idle', 'executing', 'waiting', 'pausing', 'paused', 'interrupted', 'failed'] as const
const executionStatuses = ['starting', 'running', 'waiting_approval', 'ending', 'ended'] as const
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
const messageRoles = ['user', 'assistant', 'tool'] as const
const blockKinds = ['text', 'thinking', 'tool_call', 'tool_result'] as const
const historyKinds = ['message', 'execution', 'request_canceled'] as const

function normalizeExecutionSnapshot(value: unknown): ExecutionSnapshot {
    const record = requiredRecord(value)
    return {
        sourceTaskId: typeof record.sourceTaskId === 'string' ? record.sourceTaskId : '',
        id: requiredString(record.id),
        status: enumValue(record.status, executionStatuses, 'unknown'),
        outcome: enumValue(record.outcome, executionOutcomes, 'unknown'),
        failureCode: failureCodeValue(record.failureCode),
        failureMessage: optionalString(record.failureMessage),
        createdAt: requiredString(record.createdAt),
        startedAt: optionalString(record.startedAt),
        endedAt: optionalString(record.endedAt),
    }
}

function normalizeAgentMessage(value: unknown): AgentMessage {
    const record = requiredRecord(value)
    return {
        id: optionalString(record.id),
        sequence: requiredNumber(record.sequence),
        at: requiredString(record.at),
        executionId: requiredString(record.executionId),
        role: enumValue(record.role, messageRoles, 'unknown'),
        content: optionalString(record.content),
        thinking: optionalString(record.thinking),
        blocks: record.blocks == null ? [] : requiredArray(record.blocks).map(normalizeAgentMessageBlock),
    }
}

function normalizeAgentMessageBlock(value: unknown): AgentMessageBlock {
    const record = requiredRecord(value)
    return {
        kind: enumValue(record.kind, blockKinds, 'unknown'),
        text: optionalString(record.text),
        callId: optionalString(record.callId),
        name: optionalString(record.name),
        input: record.input,
        isError: typeof record.isError === 'boolean' ? record.isError : undefined,
    }
}

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

function optionalString(value: unknown): string {
    return typeof value === 'string' ? value : ''
}

function optionalNumber(value: unknown): number | undefined {
    return value == null ? undefined : requiredNumber(value)
}

function requiredNumber(value: unknown): number {
    if (typeof value !== 'number' || !Number.isFinite(value)) {
        throw new Error('Agent 数据格式无效。')
    }
    return value
}

function optionalStringArray(value: unknown): string[] {
    if (value == null) {
        return []
    }
    if (!Array.isArray(value) || !value.every((item) => typeof item === 'string')) {
        throw new Error('Agent 数据格式无效。')
    }
    return value
}
