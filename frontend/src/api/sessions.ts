import { getSessionBinding } from './bindings'
import type { SessionSnapshot as GeneratedSessionSnapshot } from '../../bindings/praxis/wails/bindings'
import type { HistoryPage } from './agents'
import { parseToolApproval, type RawToolApproval, type ToolApproval } from './commands'
import { normalizeModelOptions, type ModelOption } from './models'
import type { QueuedTask } from './tasks'

export type SessionSnapshot = Omit<
    GeneratedSessionSnapshot,
    'history' | 'tasks' | 'models' | 'modelSelection' | 'approvalMode' | 'pendingApprovals'
> & {
    history: HistoryPage
    tasks: QueuedTask[]
    models: ModelOption[]
    modelSelection: ModelSelection
    approvalMode: SessionApprovalMode
    pendingApprovals: ToolApproval[]
}

export type SessionSummary = {
    id: string
    projectId: string
    title: string
    createdAt: string
    updatedAt: string
}

export type CreateSessionRequest = { sessionId: string; projectId: string }
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

/** 保存会话覆盖；null 恢复继承，有效模式在显式 /reload 后更新。 */
export function setSessionApprovalMode(sessionId: string, mode: ApprovalMode | null) {
    return getSessionBinding().SetSessionApprovalMode({ sessionId, mode }) as Promise<SessionApprovalMode>
}

export function selectSessionModel(sessionID: string, selection: ModelSelection) {
    return getSessionBinding().SelectModel({ sessionId: sessionID, ...selection })
}

/** 后端快照一次返回首屏历史、模型目录、模型选择、审批模式和挂起审批。 */
export async function selectSession(sessionID: string): Promise<SessionSnapshot> {
    const raw = await getSessionBinding().SelectSession(sessionID) as unknown as Omit<
        SessionSnapshot,
        'models' | 'pendingApprovals'
    > & {
        models?: ModelOption[]
        pendingApprovals?: RawToolApproval[]
    }
    return {
        ...raw,
        models: normalizeModelOptions(raw.models),
        pendingApprovals: (raw.pendingApprovals ?? []).map(parseToolApproval),
    }
}

export function closeSession(sessionID: string) { return getSessionBinding().CloseSession(sessionID) }
export function deleteSession(sessionID: string) { return getSessionBinding().DeleteSession(sessionID) }
export function renameSession(sessionID: string, title: string) { return getSessionBinding().RenameSession(sessionID, title) }

/** 在系统文件管理器中定位指定 Session 的 JSONL 文件；失败时拒绝 Promise。 */
export function revealSessionFile(sessionID: string) { return getSessionBinding().RevealSessionFile(sessionID) }
