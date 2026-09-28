import { EventsOn } from '../../wailsjs/runtime/runtime'
import { getAgentBinding, isBindingAvailable } from './bindings'

export const AGENT_EVENT_NAME = 'praxis:agent-event'

export type AgentSnapshot = {
    id: string
    sessionId: string
    definitionId: string
    securityPolicyRevision: number
    profile: string
    state: string
    currentExecutionId: string
    executionIds: string[]
    executions: ExecutionSnapshot[]
    waitConditionIds: string[]
    deliveryIds: string[]
    controlCommandIds: string[]
}

export type ExecutionSnapshot = {
    id: string
    reason: string
    status: string
    outcome: string
    failureCode: string
    createdAt: string
    startedAt: string
    settledAt: string
}

export type AgentMessage = {
    sequence: number
    at: string
    executionId: string
    role: string
    content: string
    thinking?: string
    blocks?: AgentMessageBlock[]
}

export type AgentMessageBlock = {
    kind: 'text' | 'thinking' | 'tool_call' | 'tool_result'
    text?: string
    callId?: string
    name?: string
    input?: unknown
    isError?: boolean
}

export type AgentHistoryItem = {
    kind: 'message' | 'execution'
    sequence: number
    at: string
    message?: AgentMessage
    execution?: ExecutionSnapshot
}

export type AgentEvent = {
    kind: 'turn_started' | 'text_delta' | 'thinking_delta' | 'tool_call' | 'tool_result' | 'turn_completed' | 'settled' | 'error'
    agentId: string
    executionId: string
    turn?: number
    text?: string
    callId?: string
    name?: string
    input?: unknown
    result?: string
    isError?: boolean
    error?: string
}

export function getAgent(agentID: string) {
    return getAgentBinding().GetAgent(agentID)
}

export function listAgentMessages(agentID: string) {
    return getAgentBinding().ListAgentMessages(agentID)
}

export function listAgentHistory(agentID: string) {
    return getAgentBinding().ListAgentHistory(agentID)
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
    if (value.kind !== 'turn_started' && value.kind !== 'text_delta' && value.kind !== 'thinking_delta' &&
        value.kind !== 'tool_call' && value.kind !== 'tool_result' && value.kind !== 'turn_completed' &&
        value.kind !== 'settled' && value.kind !== 'error') {
        return null
    }
    if ((value.kind === 'text_delta' || value.kind === 'thinking_delta') && typeof value.text !== 'string') {
        return null
    }
    if ((value.kind === 'tool_call' || value.kind === 'tool_result') &&
        (typeof value.callId !== 'string' || typeof value.name !== 'string')) {
        return null
    }
    return {
        kind: value.kind,
        agentId: value.agentId,
        executionId: value.executionId,
        turn: typeof value.turn === 'number' ? value.turn : undefined,
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
