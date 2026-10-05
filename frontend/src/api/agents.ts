import { EventsOn } from '../../wailsjs/runtime/runtime'
import { getAgentBinding, isBindingAvailable } from './bindings'
import { normalizeOperationTiming, type OperationTiming } from './timings'

export const AGENT_EVENT_NAME = 'praxis:agent-event'

export type AgentState = 'idle' | 'executing' | 'waiting' | 'pausing' | 'paused' | 'interrupted' | 'failed' | 'closed' | 'unknown'
export type TurnReason = 'user_input' | 'queued_work' | 'resume' | 'unknown'
export type TurnStatus = 'starting' | 'running' | 'settling' | 'settled' | 'unknown'
export type TurnOutcome = '' | 'completed' | 'yielded' | 'paused' | 'failed' | 'interrupted' | 'unknown'
export type TurnFailureCode =
    | ''
    | 'provider_unavailable'
    | 'turn_provider_error'
    | 'turn_tool_error'
    | 'turn_policy_blocked'
    | 'turn_approval_required'
    | 'turn_resource_limit'
    | 'turn_storage_error'
    | 'turn_contract_error'
    | 'turn_busy'
    | 'turn_closed'
    | 'turn_interrupted'
    | 'runtime_cancelled'
    | 'runtime_failed'
    | 'runtime_invalid_outcome'
    | 'unknown'
export type AgentMessageRole = 'user' | 'assistant' | 'tool' | 'unknown'
export type AgentMessageBlockKind = 'text' | 'thinking' | 'tool_call' | 'tool_result' | 'unknown'
export type AgentHistoryKind = 'message' | 'turn' | 'unknown'

export type AgentSnapshot = {
    id: string
    name: string
    sessionId: string
    definitionId: string
    securityPolicyRevision: number
    profile: string
    state: AgentState
    currentTurnId: string
    turnIds: string[]
    turns: TurnSnapshot[]
    waitConditionIds: string[]
    deliveryIds: string[]
    controlCommandIds: string[]
}

export type TurnSnapshot = {
    id: string
    reason: TurnReason
    status: TurnStatus
    outcome: TurnOutcome
    failureCode: TurnFailureCode
    failureMessage: string
    createdAt: string
    startedAt: string
    settledAt: string
}

export type AgentMessage = {
    id?: string
    sequence: number
    at: string
    turnId: string
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
    turn?: TurnSnapshot
}

export type ModelUsage = {
    inputTokens: number
    outputTokens?: number
    cacheReadInputTokens?: number
    cacheCreationInputTokens?: number
}

export type ModelUsageRecord = {
    sessionId: string
    agentId: string
    turnId: string
    step: number
    usage: ModelUsage
}

export type AgentEvent = {
    kind: 'step_started' | 'provider_waiting' | 'text_delta' | 'thinking_delta' | 'tool_call' | 'tool_result' | 'step_completed' | 'model_usage' | 'settled' | 'error' | 'operation_timing'
    timing?: OperationTiming
    usage?: ModelUsage
    sessionId?: string
    agentId: string
    turnId: string
    step?: number
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

export function listAgentHistory(agentID: string) {
    return getAgentBinding().ListAgentHistory(agentID).then(normalizeAgentHistory)
}

/** 查询会话全部请求的用量；缺失计数保留为未知，不推算 token。 */
export async function listSessionUsage(sessionId: string): Promise<ModelUsageRecord[]> {
    const values: unknown = await getAgentBinding().ListSessionUsage(sessionId)
    return requiredArray(values).map((value) => {
        const record = requiredRecord(value)
        const usage = normalizeModelUsage(record.usage)
        if (record.sessionId !== sessionId || !Number.isSafeInteger(record.step) || Number(record.step) < 1 || !usage) {
            throw new Error('Token 用量数据格式无效。')
        }
        const agentId = requiredString(record.agentId)
        const turnId = requiredString(record.turnId)
        if (!agentId || !turnId) throw new Error('Token 用量数据格式无效。')
        return { sessionId, agentId, turnId, step: record.step as number, usage }
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
        securityPolicyRevision: requiredNumber(record.securityPolicyRevision),
        profile: requiredString(record.profile),
        state: enumValue(record.state, agentStates, 'unknown'),
        currentTurnId: requiredString(record.currentTurnId),
        turnIds: optionalStringArray(record.turnIds),
        turns: requiredArray(record.turns).map(normalizeTurnSnapshot),
        waitConditionIds: optionalStringArray(record.waitConditionIds),
        deliveryIds: optionalStringArray(record.deliveryIds),
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
            turn: record.turn == null ? undefined : normalizeTurnSnapshot(record.turn),
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
        typeof value.turnId !== 'string') {
        return null
    }
    if (value.kind !== 'step_started' && value.kind !== 'provider_waiting' && value.kind !== 'text_delta' && value.kind !== 'thinking_delta' &&
        value.kind !== 'tool_call' && value.kind !== 'tool_result' && value.kind !== 'step_completed' &&
        value.kind !== 'model_usage' && value.kind !== 'settled' && value.kind !== 'error' && value.kind !== 'operation_timing') {
        return null
    }
    if ((value.kind === 'text_delta' || value.kind === 'thinking_delta') && typeof value.text !== 'string') {
        return null
    }
    if ((value.kind === 'tool_call' || value.kind === 'tool_result') &&
        (typeof value.callId !== 'string' || typeof value.name !== 'string')) {
        return null
    }
    const reportedUsage = value.kind === 'model_usage' ? normalizeModelUsage(value.usage) : undefined
    if (value.kind === 'model_usage' && (!reportedUsage || !Number.isSafeInteger(value.step) || Number(value.step) < 1 ||
        typeof value.sessionId !== 'string' || !value.sessionId || !value.agentId || !value.turnId)) return null
    let timing: OperationTiming | undefined
    if (value.kind === 'operation_timing') {
        try {
            timing = normalizeOperationTiming(value.timing)
            if (timing.agentId !== value.agentId || timing.turnId !== value.turnId) return null
        } catch {
            return null
        }
    }
    return {
        kind: value.kind,
        timing,
        usage: reportedUsage ?? undefined,
        sessionId: typeof value.sessionId === 'string' ? value.sessionId : undefined,
        agentId: value.agentId,
        turnId: value.turnId,
        step: typeof value.step === 'number' ? value.step : undefined,
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

const agentStates = ['idle', 'executing', 'waiting', 'pausing', 'paused', 'interrupted', 'failed', 'closed'] as const
const turnReasons = ['user_input', 'queued_work', 'resume'] as const
const turnStatuses = ['starting', 'running', 'settling', 'settled'] as const
const turnOutcomes = ['', 'completed', 'yielded', 'paused', 'failed', 'interrupted'] as const
const turnFailureCodes = [
    '',
    'provider_unavailable',
    'turn_provider_error',
    'turn_tool_error',
    'turn_policy_blocked',
    'turn_approval_required',
    'turn_resource_limit',
    'turn_storage_error',
    'turn_contract_error',
    'turn_busy',
    'turn_closed',
    'turn_interrupted',
    'runtime_cancelled',
    'runtime_failed',
    'runtime_invalid_outcome',
] as const
const messageRoles = ['user', 'assistant', 'tool'] as const
const blockKinds = ['text', 'thinking', 'tool_call', 'tool_result'] as const
const historyKinds = ['message', 'turn'] as const

function normalizeTurnSnapshot(value: unknown): TurnSnapshot {
    const record = requiredRecord(value)
    return {
        id: requiredString(record.id),
        reason: enumValue(record.reason, turnReasons, 'unknown'),
        status: enumValue(record.status, turnStatuses, 'unknown'),
        outcome: enumValue(record.outcome, turnOutcomes, 'unknown'),
        failureCode: failureCodeValue(record.failureCode),
        failureMessage: optionalString(record.failureMessage),
        createdAt: requiredString(record.createdAt),
        startedAt: optionalString(record.startedAt),
        settledAt: optionalString(record.settledAt),
    }
}

function normalizeAgentMessage(value: unknown): AgentMessage {
    const record = requiredRecord(value)
    return {
        id: optionalString(record.id),
        sequence: requiredNumber(record.sequence),
        at: requiredString(record.at),
        turnId: requiredString(record.turnId),
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

function failureCodeValue(value: unknown): TurnFailureCode {
    if (value == null) {
        return ''
    }
    return enumValue(value, turnFailureCodes, 'unknown')
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
