import { getSessionBinding } from './bindings'
import { normalizeAgentSnapshot, type AgentSnapshot } from './agents'

export type SessionSnapshot = {
    id: string
    projectId: string
    workspaceId: string
    title: string
    createdAt: string
    updatedAt: string
    agents: AgentSnapshot[]
}

export type SessionSummary = {
    id: string
    projectId: string
    workspaceId: string
    title: string
    createdAt: string
    updatedAt: string
}

export type CreateSessionRequest = {
    sessionId: string
    agentId: string
    projectId: string
    workspaceId: string
    agentDefinitionId: string
    requestId: string
}

export function listSessions(projectID: string) {
    return getSessionBinding().ListSessions(projectID)
}

export function createSession(request: CreateSessionRequest) {
    return getSessionBinding().CreateSession(request)
}

export function getSession(sessionID: string) {
    return getSessionBinding().GetSession(sessionID).then(normalizeSessionSnapshot)
}

export function deleteSession(sessionID: string) {
    return getSessionBinding().DeleteSession(sessionID)
}

export function renameSession(sessionID: string, title: string) {
    return getSessionBinding().RenameSession(sessionID, title)
}

function normalizeSessionSnapshot(value: unknown): SessionSnapshot {
    if (!isRecord(value) || !Array.isArray(value.agents)) {
        throw new Error('Session 数据格式无效。')
    }
    return {
        id: requiredString(value.id),
        projectId: requiredString(value.projectId),
        workspaceId: requiredString(value.workspaceId),
        title: requiredString(value.title),
        createdAt: requiredString(value.createdAt),
        updatedAt: requiredString(value.updatedAt),
        agents: value.agents.map(normalizeAgentSnapshot),
    }
}

function isRecord(value: unknown): value is Record<string, unknown> {
    return typeof value === 'object' && value !== null
}

function requiredString(value: unknown): string {
    if (typeof value !== 'string') {
        throw new Error('Session 数据格式无效。')
    }
    return value
}
