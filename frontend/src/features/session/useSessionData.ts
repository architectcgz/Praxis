import { useCallback, useEffect, useRef, useState } from 'react'
import { closeSession, selectSession, subscribeAgentEvents, type SessionSnapshot } from '../../api'

/** 工作区按 SessionID 保留唯一会话页；切换显示不改变会话数据的生命周期。 */
export function useSessionData(sessionID: string) {
    const [session, setSession] = useState<SessionSnapshot | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<unknown>()
    const request = useRef(0)
    const selectedBranchID = useRef('')
    const pendingLoad = useRef<Promise<void> | null>(null)

    const loadSession = useCallback(async () => {
        const id = ++request.current
        setLoading(true)
        setError(undefined)
        try {
            const pending = selectSession(sessionID, selectedBranchID.current)
            pendingLoad.current = pending.then(() => undefined, () => undefined)
            const value = await pending
            if (request.current !== id) return
            selectedBranchID.current = value.branch.id
            setSession(value)
            return value
        } catch (reason) {
            if (request.current === id) setError(() => reason)
            throw reason
        } finally {
            if (request.current === id) setLoading(false)
        }
    }, [sessionID])

    const releaseSession = useCallback(async () => {
        // 打开请求结束后再关闭，确保初始化失败或尚未发送消息的页面也能释放后端数据。
        await pendingLoad.current
        await closeSession(sessionID)
    }, [sessionID])

    const adoptSession = useCallback((value: SessionSnapshot) => {
        request.current += 1
        selectedBranchID.current = value.branch.id
        setSession(value)
        setLoading(false)
        setError(undefined)
    }, [])

    const bindSubmission = useCallback((bindingID: string) => {
        setSession((current) => current ? { ...current, bindingId: bindingID } : current)
    }, [])

    const updateTitle = useCallback((title: string) => {
        setSession((current) => current ? { ...current, title } : current)
    }, [])

    useEffect(() => subscribeAgentEvents((event) => {
        if (event.sessionId !== sessionID) return
        if (event.kind === 'task_queue_changed') {
            setSession((current) => current && current.branch.id === event.branchId &&
                (!current.bindingId || current.bindingId === event.bindingId)
                ? { ...current, tasks: event.data.tasks } : current)
        }
        if ((event.kind === 'entries_appended' || event.kind === 'run_ended' || event.kind === 'request_canceled') && event.agentId === sessionID) {
            const last = event.data.entries.at(-1)
            if (last) {
                setSession((current) => current && current.branch.id === event.branchId ? {
                    ...current,
                    branch: { ...current.branch, head_entry_id: last.id, updated_at: last.createdAt },
                } : current)
            }
        }
    }), [sessionID])

    useEffect(() => () => { request.current += 1 }, [])
    return { session, sessionLoading: loading, sessionError: error, loadSession, releaseSession, adoptSession, bindSubmission, updateTitle }
}
