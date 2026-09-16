import { EventsOn } from '../../wailsjs/runtime/runtime'
import { getAgentBinding, isBindingAvailable } from './bindings'

export const AGENT_OUTPUT_EVENT_NAME = 'praxis:agent-output'

export type AgentSnapshot = {
    id: string
    sessionId: string
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
}

export type AgentHistoryItem = {
    kind: 'message' | 'execution'
    sequence: number
    at: string
    message?: AgentMessage
    execution?: ExecutionSnapshot
}

export type AgentOutputEvent = {
    kind: 'text_delta' | 'settled'
    agentId: string
    executionId: string
    text: string
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

export function subscribeAgentOutput(listener: (event: AgentOutputEvent) => void) {
    if (!isBindingAvailable()) {
        return () => { }
    }
    return EventsOn(AGENT_OUTPUT_EVENT_NAME, (value: unknown) => {
        const event = parseAgentOutputEvent(value)
        if (event) {
            listener(event)
        }
    })
}

function parseAgentOutputEvent(value: unknown): AgentOutputEvent | null {
    if (!isRecord(value) || typeof value.agentId !== 'string' ||
        typeof value.executionId !== 'string' || typeof value.text !== 'string') {
        return null
    }
    if (value.kind !== 'text_delta' && value.kind !== 'settled') {
        return null
    }
    return {
        kind: value.kind,
        agentId: value.agentId,
        executionId: value.executionId,
        text: value.text,
    }
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null
}
