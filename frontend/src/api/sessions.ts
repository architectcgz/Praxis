import { getSessionBinding } from './bindings'
import type { AgentView, HistoryPage } from './agents'
import type { QueuedTask } from './tasks'
import type { Branch } from '../../bindings/praxis/internal/core/session/models'
export type { Branch }

export type SessionSnapshot = {
    bindingId: string
    branch: Branch
    id: string
    projectId: string
    title: string
    createdAt: string
    updatedAt: string
    agents: AgentView[]
    tasks: QueuedTask[]
    entries: Record<string, HistoryPage>
}

export type SessionSummary = {
    id: string
    projectId: string
    title: string
    createdAt: string
    updatedAt: string
}

export type SessionUsageSummary = {
    totalRequests: number
    inputTokens: number
    outputTokens: number
    cacheReadInputTokens: number
    cacheCreationInputTokens: number
    reportedRequests: number
    outputReports: number
    cacheReadReports: number
    cacheCreationReports: number
    cacheReadRatio: number | null
    cacheReadComplete: boolean
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
export function selectSession(sessionID: string, branchID = '') {
    return getSessionBinding().SelectSession(sessionID, branchID) as Promise<SessionSnapshot>
}

export function getSessionSummary(sessionID: string) { return getSessionBinding().GetSessionSummary(sessionID) }
export function createBranch(session: SessionSnapshot, entryId: string, name = '', requestId: string = crypto.randomUUID()) {
    return getSessionBinding().CreateBranch({ sessionId: session.id, branchId: session.branch.id, bindingId: session.bindingId, entryId, name, requestId }) as Promise<SessionSnapshot>
}
export function switchBranch(session: SessionSnapshot, name: string) {
    return getSessionBinding().SwitchBranch({ sessionId: session.id, branchId: session.branch.id, bindingId: session.bindingId, name }) as Promise<SessionSnapshot>
}
export async function listBranches(sessionId: string) { return await getSessionBinding().ListBranches(sessionId) ?? [] }
export async function getEntryTree(sessionId: string) { return await getSessionBinding().GetEntryTree(sessionId) ?? [] }
export function getEntriesAt(sessionId: string, entryId: string, before = '') {
    return getSessionBinding().GetEntriesAt({ sessionId, headEntryId: entryId, beforeEntryId: before, limit: 50 }) as Promise<HistoryPage>
}
export function deleteBranch(session: SessionSnapshot, targetBranchId: string) {
    return getSessionBinding().DeleteBranch({ sessionId: session.id, branchId: session.branch.id, targetBranchId, bindingId: session.bindingId })
}

export function getSessionUsageSummary(sessionID: string) {
    return getSessionBinding().GetSessionUsageSummary(sessionID) as Promise<SessionUsageSummary>
}

export function closeSession(sessionID: string) { return getSessionBinding().CloseSession(sessionID) }
export function deleteSession(sessionID: string) { return getSessionBinding().DeleteSession(sessionID) }
export function renameSession(sessionID: string, title: string) { return getSessionBinding().RenameSession(sessionID, title) }

/** 在系统文件管理器中定位指定 Session 的 JSONL 文件；失败时拒绝 Promise。 */
export function revealSessionFile(sessionID: string) { return getSessionBinding().RevealSessionFile(sessionID) }
