import { useCallback, useEffect, useRef, useState } from 'react'
import { getSessionSummary, listSessions, type SessionSummary } from '../../api'

/** 会话目录只持有元数据，不拥有页面的历史、计时或执行状态。 */
export function useSessionCatalog() {
    const [sessions, setSessions] = useState<SessionSummary[]>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState<unknown>()
    const catalog = useRef<SessionSummary[]>([])
    const request = useRef(0)
    const metadataRequests = useRef(new Map<string, number>())

    const refreshSessions = useCallback(async (projectIDs: readonly string[]) => {
        const id = ++request.current
        setLoading(true)
        try {
            const pages = await Promise.all(projectIDs.map(listSessions))
            if (request.current !== id) return
            const values = pages.flat().sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
            catalog.current = values
            setSessions(values)
            setError(undefined)
            return values
        } catch (reason) {
            if (request.current === id) setError(() => reason)
            throw reason
        } finally {
            if (request.current === id) setLoading(false)
        }
    }, [])

    const refreshSummary = useCallback(async (id: string) => {
        if (!catalog.current.some((item) => item.id === id)) return
        const version = (metadataRequests.current.get(id) || 0) + 1
        metadataRequests.current.set(id, version)
        try {
            const summary = await getSessionSummary(id)
            if (metadataRequests.current.get(id) !== version) return
            const values = catalog.current.map((item) => item.id === id ? summary : item)
                .sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
            catalog.current = values
            setSessions(values)
        } catch (reason) {
            if (metadataRequests.current.get(id) === version) setError(() => reason)
        }
    }, [])

    const updateSessionTitle = useCallback((id: string, title: string) => {
        metadataRequests.current.set(id, (metadataRequests.current.get(id) || 0) + 1)
        const values = catalog.current.map((item) => item.id === id ? { ...item, title } : item)
        catalog.current = values
        setSessions(values)
    }, [])

    useEffect(() => () => { request.current += 1; metadataRequests.current.clear() }, [])
    return { sessions, loading, error, refreshSessions, refreshSummary, updateSessionTitle }
}
