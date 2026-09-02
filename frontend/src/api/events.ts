import { getEventBinding } from './bindings'

export type EventSnapshot = {
    id: string
    type: string
    occurredAt: string
    sessionId?: string
    agentId?: string
    executionId?: string
    payload?: Record<string, string>
}

export function listSessionEvents(sessionID: string, after?: string, limit = 200) {
    return getEventBinding().ListSessionEvents(sessionID, after || new Date(0).toISOString(), limit)
}

export function listAgentEvents(agentID: string, after?: string, limit = 200) {
    return getEventBinding().ListAgentEvents(agentID, after || new Date(0).toISOString(), limit)
}

export function listExecutionEvents(executionID: string, after?: string, limit = 200) {
    return getEventBinding().ListExecutionEvents(executionID, after || new Date(0).toISOString(), limit)
}
