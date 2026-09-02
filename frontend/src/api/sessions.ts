import { getSessionBinding } from './bindings'
import type { AgentSnapshot } from './agents'

export type SessionSnapshot = {
    id: string
    projectId: string
    workspaceId: string
    goal: string
    createdAt: string
    updatedAt: string
    agents: AgentSnapshot[]
}

export type SessionSummary = {
    id: string
    projectId: string
    workspaceId: string
    goal: string
    createdAt: string
    updatedAt: string
}

export type CreateSessionRequest = { projectId: string; workspaceId: string; goal: string; requestId: string }

export type CreateSessionResponse = {
    sessionId: string
    projectId: string
    workspaceId: string
    agentId: string
    goal: string
}

export function listSessions(projectID: string) {
    return getSessionBinding().ListSessions(projectID)
}

export function createSession(request: CreateSessionRequest) {
    return getSessionBinding().CreateSession(request)
}

export function getSession(sessionID: string) {
    return getSessionBinding().GetSession(sessionID)
}
