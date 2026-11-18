import { getSessionBinding } from './bindings'
import type { SessionUsageSummary as GeneratedSessionUsageSummary } from '../../bindings/praxis/internal/dto/models'
import type { AgentView, HistoryPage } from './agents'
import type { QueuedTask } from './tasks'

export type SessionUsageSummary = Omit<GeneratedSessionUsageSummary, 'historyEnds'> & { historyEnds: Record<string, number> }

export type SessionSnapshot = {
    queuePaused: boolean
    id: string
    projectId: string
    title: string
    createdAt: string
    updatedAt: string
    agents: AgentView[]
    tasks: QueuedTask[]
    history: HistoryPage
    usage: SessionUsageSummary
}

export type SessionSummary = {
    id: string
    projectId: string
    title: string
    createdAt: string
    updatedAt: string
}

export type CreateSessionRequest = { sessionId: string; projectId: string; requestId: string }
export type ApprovalMode = 'ask' | 'auto' | 'yolo'
export type SessionApprovalMode = {
    sessionId: string
    globalMode: ApprovalMode
    sessionMode: ApprovalMode | null
    effectiveMode: ApprovalMode
}
export type ModelSelection = { providerId: string; modelId: string; reasoningLevel: string }

export async function listSessions(projectID: string) {
    return await getSessionBinding().ListSessions(projectID) ?? []
}

export function createSession(request: CreateSessionRequest) { return getSessionBinding().CreateSession(request) }

/** 读取后端解析的会话选择和有效模式，不在前端推断授权结果。 */
export function getSessionApprovalMode(sessionId: string) {
    return getSessionBinding().GetSessionApprovalMode(sessionId) as Promise<SessionApprovalMode>
}

/** 保存会话覆盖；null 恢复继承，有效模式在显式 /reload 后更新。 */
export function setSessionApprovalMode(sessionId: string, mode: ApprovalMode | null) {
    return getSessionBinding().SetSessionApprovalMode({ sessionId, mode }) as Promise<SessionApprovalMode>
}

export function selectSessionModel(sessionID: string, selection: ModelSelection) {
    return getSessionBinding().SelectModel({ sessionId: sessionID, ...selection })
}

export function getSessionModelSelection(sessionID: string): Promise<ModelSelection> {
    return getSessionBinding().GetModelSelection(sessionID)
}

/** 后端快照投影保证集合为数组；前端直接接纳，不重新校验内部协议。 */
export function selectSession(sessionID: string) {
    return getSessionBinding().SelectSession(sessionID) as Promise<SessionSnapshot>
}

export function getSessionSummary(sessionID: string) { return getSessionBinding().GetSessionSummary(sessionID) }
export function closeSession(sessionID: string) { return getSessionBinding().CloseSession(sessionID) }
export function deleteSession(sessionID: string) { return getSessionBinding().DeleteSession(sessionID) }
export function renameSession(sessionID: string, title: string) { return getSessionBinding().RenameSession(sessionID, title) }

/** 在系统文件管理器中定位指定 Session 的 JSONL 文件；失败时拒绝 Promise。 */
export function revealSessionFile(sessionID: string) { return getSessionBinding().RevealSessionFile(sessionID) }
