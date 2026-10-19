import { useCallback, useEffect, useRef, useState } from 'react'
import { getSession, listSessions, type SessionSnapshot, type SessionSummary } from '../../api'

/** Session 拥有目录、详情和摘要回填；工作区只负责选择与跨模块调用顺序。 */
export function useSessionData() {
    const [sessions, setSessions] = useState<SessionSummary[]>([])
    const [session, setSession] = useState<SessionSnapshot | null>(null)
    const [catalogLoading, setCatalogLoading] = useState(false)
    const [sessionLoading, setSessionLoading] = useState(false)
    const [catalogError, setCatalogError] = useState<unknown>()
    const [sessionError, setSessionError] = useState<unknown>()
    const catalog = useRef<SessionSummary[]>([])
    const catalogRequest = useRef(0)
    const detailRequest = useRef(0)
    const detailID = useRef('')
    const metadataRequests = useRef(new Map<string, number>())

    const getSessions = useCallback((): readonly SessionSummary[] => catalog.current, [])
    const clearSession = useCallback(() => {
        detailRequest.current += 1
        detailID.current = ''
        setSession(null)
        setSessionLoading(false)
        setSessionError(undefined)
    }, [])

    const refreshSessions = useCallback(async (projectIDs: readonly string[]) => {
        const request = ++catalogRequest.current
        setCatalogLoading(true)
        try {
            const pages = await Promise.all(projectIDs.map((id) => listSessions(id)))
            if (catalogRequest.current !== request) return
            const values = pages.flat().sort((left, right) => Date.parse(right.updatedAt) - Date.parse(left.updatedAt))
            catalog.current = values
            setSessions(values)
            setCatalogError(undefined)
            setSession((current) => {
                const summary = values.find((item) => item.id === current?.id)
                return current && summary ? { ...current, ...summary } : current
            })
            return values
        } catch (reason) {
            if (catalogRequest.current === request) setCatalogError(() => reason)
            throw reason
        } finally {
            if (catalogRequest.current === request) setCatalogLoading(false)
        }
    }, [])

    const loadSession = useCallback(async (id: string) => {
        const request = ++detailRequest.current
        detailID.current = id
        setSessionLoading(true)
        setSessionError(undefined)
        try {
            const value = await getSession(id)
            if (detailRequest.current !== request) return
            setSession(value)
            return value
        } catch (reason) {
            if (detailRequest.current === request) {
                setSession(null)
                setSessionError(() => reason)
            }
            throw reason
        } finally {
            if (detailRequest.current === request) setSessionLoading(false)
        }
    }, [])

    const refreshSummary = useCallback(async (id: string) => {
        if (!catalog.current.some((item) => item.id === id)) return
        const request = (metadataRequests.current.get(id) || 0) + 1
        metadataRequests.current.set(id, request)
        const catalogVersion = catalogRequest.current
        const isCurrent = () => metadataRequests.current.get(id) === request && catalogRequest.current === catalogVersion
        try {
            const summary = await getSession(id)
            if (!isCurrent() || !catalog.current.some((item) => item.id === id)) return
            const values = catalog.current.map((item) => item.id === id ? {
                ...item, title: summary.title, updatedAt: summary.updatedAt,
            } : item).sort((left, right) => Date.parse(right.updatedAt) - Date.parse(left.updatedAt))
            catalog.current = values
            setSessions(values)
            setSession((current) => current?.id === id ? { ...current, title: summary.title, updatedAt: summary.updatedAt } : current)
        } catch (reason) {
            if (isCurrent() && detailID.current === id) setSessionError(() => reason)
        }
    }, [])

    const updateSessionTitle = useCallback((id: string, title: string) => {
        // 用户确认的标题优先于此前启动的摘要请求。
        metadataRequests.current.set(id, (metadataRequests.current.get(id) || 0) + 1)
        setSession((current) => current?.id === id ? { ...current, title } : current)
        const values = catalog.current.map((item) => item.id === id ? { ...item, title } : item)
        catalog.current = values
        setSessions(values)
    }, [])

    const resetSessions = useCallback(() => {
        catalogRequest.current += 1
        metadataRequests.current.clear()
        catalog.current = []
        setSessions([])
        setCatalogLoading(false)
        setCatalogError(undefined)
        clearSession()
    }, [clearSession])

    useEffect(() => () => {
        catalogRequest.current += 1
        detailRequest.current += 1
        metadataRequests.current.clear()
    }, [])

    return {
        sessions, session, catalogLoading, sessionLoading, catalogError, sessionError,
        getSessions, refreshSessions, loadSession, refreshSummary, updateSessionTitle, clearSession, resetSessions,
    }
}
