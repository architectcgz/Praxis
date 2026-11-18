import { useCallback, useEffect, useRef, useState, useSyncExternalStore } from 'react'
import { closeSession, selectSession, subscribeAgentEvents } from '../../api'
import type { SessionStore } from './sessionStore'

/** 先订阅再请求初始快照，保留加载期间到达的通知。 */
export function useSessionData(store: SessionStore) {
    const data = useSyncExternalStore(store.subscribeData, store.getData)
    const [loading, setLoading] = useState(true)
    const [error, setError] = useState<unknown>()
    const [closing, setClosing] = useState(false)
    const request = useRef(0)
    const pendingLoad = useRef<Promise<unknown>>(Promise.resolve())
    const loadSession = useCallback(async () => {
        store.beginLoad()
        const id = ++request.current
        setLoading(true)
        setError(undefined)
        try {
            const pending = selectSession(store.sessionId)
            pendingLoad.current = pending
            const value = await pending
            if (request.current === id) store.initialize(value)
        } catch (reason) {
            if (request.current === id) setError(() => reason)
        } finally { if (request.current === id) setLoading(false) }
    }, [store])
    useEffect(() => {
        const unsubscribe = subscribeAgentEvents(store.sessionId, store.receive)
        void loadSession()
        return () => { request.current += 1; unsubscribe() }
    }, [store, loadSession])
    const releaseSession = useCallback(async () => {
        setClosing(true)
        try {
            await pendingLoad.current.catch(() => undefined)
            await closeSession(store.sessionId)
            return true
        } catch (reason) { setError(() => reason); return false }
        finally { setClosing(false) }
    }, [store])
    return { ...data, sessionLoading: loading, sessionError: error, closing, releaseSession, updateTitle: store.updateTitle }
}
