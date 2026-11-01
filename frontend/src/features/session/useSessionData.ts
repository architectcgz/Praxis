import { useCallback, useEffect, useRef, useState } from 'react'
import { selectSession, subscribeAgentEvents, type SessionSnapshot } from '../../api'

/** 每个页面持有一份会话选择；切换到其他页面不会销毁本 Hook。 */
export function useSessionData(sessionID: string) {
    const [session, setSession] = useState<SessionSnapshot | null>(null)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<unknown>()
    const request = useRef(0)
    const selection = useRef({ branchID: '', bindingID: '' })

    const loadSession = useCallback(async () => {
        const id = ++request.current
        setLoading(true)
        setError(undefined)
        try {
            const current = selection.current
            const value = await selectSession(sessionID, current.branchID, current.bindingID)
            if (request.current !== id) return
            selection.current = { branchID: value.branch.id, bindingID: value.bindingId }
            setSession(value)
            return value
        } catch (reason) {
            if (request.current === id) setError(() => reason)
            throw reason
        } finally {
            if (request.current === id) setLoading(false)
        }
    }, [sessionID])

    const adoptSession = useCallback((value: SessionSnapshot) => {
        request.current += 1
        selection.current = { branchID: value.branch.id, bindingID: value.bindingId }
        setSession(value)
        setLoading(false)
        setError(undefined)
    }, [])

    const bindSubmission = useCallback((bindingID: string) => {
        selection.current = { ...selection.current, bindingID }
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
        if (event.kind === 'entries_appended' && !event.data.transient ||
            event.kind === 'execution_ended' || event.kind === 'request_canceled') {
            const last = event.data.entries.at(-1)
            if (last && event.agentId === sessionID) {
                setSession((current) => current && current.branch.id === event.branchId ? {
                    ...current,
                    branch: { ...current.branch, head_entry_id: last.id, updated_at: last.createdAt },
                } : current)
            }
        }
    }), [sessionID])

    useEffect(() => () => { request.current += 1 }, [])
    return { session, sessionLoading: loading, sessionError: error, loadSession, adoptSession, bindSubmission, updateTitle }
}
