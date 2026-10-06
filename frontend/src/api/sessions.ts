import { getSessionBinding } from './bindings'
import { normalizeAgentSnapshot, normalizeModelUsageRecords, type AgentSnapshot, type ModelUsageRecord } from './agents'

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

export type SessionUsageSummary = {
    records: ModelUsageRecord[]
    inputTokens: number
    cacheReadInputTokens: number
    cacheReadRatio: number | null
    cacheReadComplete: boolean
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

/** 查询同一份持久化快照的明细与缓存率；未知比例保留为 null。 */
export async function getSessionUsageSummary(sessionID: string): Promise<SessionUsageSummary> {
    const value: unknown = await getSessionBinding().GetSessionUsageSummary(sessionID)
    if (!isRecord(value) || !Number.isSafeInteger(value.inputTokens) || Number(value.inputTokens) < 0 ||
        !Number.isSafeInteger(value.cacheReadInputTokens) || Number(value.cacheReadInputTokens) < 0 ||
        Number(value.cacheReadInputTokens) > Number(value.inputTokens) || typeof value.cacheReadComplete !== 'boolean' ||
        (value.cacheReadRatio !== null && (typeof value.cacheReadRatio !== 'number' || !Number.isFinite(value.cacheReadRatio) ||
            value.cacheReadRatio < 0 || value.cacheReadRatio > 1 || !value.cacheReadComplete || value.inputTokens === 0))) {
        throw new Error('会话用量汇总数据格式无效。')
    }
    return {
        records: normalizeModelUsageRecords(value.records, sessionID),
        inputTokens: value.inputTokens as number,
        cacheReadInputTokens: value.cacheReadInputTokens as number,
        cacheReadRatio: value.cacheReadRatio as number | null,
        cacheReadComplete: value.cacheReadComplete,
    }
}

export function deleteSession(sessionID: string) {
    return getSessionBinding().DeleteSession(sessionID)
}

export function renameSession(sessionID: string, title: string) {
    return getSessionBinding().RenameSession(sessionID, title)
}

/** 在系统文件管理器中定位指定 Session 的 JSONL 文件；失败时拒绝 Promise。 */
export function revealSessionFile(sessionID: string) {
    return getSessionBinding().RevealSessionFile(sessionID)
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
