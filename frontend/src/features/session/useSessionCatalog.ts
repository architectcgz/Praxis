import { useCallback, useEffect, useRef, useState } from 'react'
import { listSessions, type SessionSummary } from '../../api'

/** 会话目录只持有元数据，不拥有页面的历史、计时或执行状态。 */
export function useSessionCatalog() {
    const [sessions, setSessions] = useState<SessionSummary[]>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState<unknown>()
    const catalog = useRef<SessionSummary[]>([])
    const request = useRef(0)

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

    const updateSessionTitle = useCallback((id: string, title: string, updatedAt?: string) => {
        if (!catalog.current.some((item) => item.id === id)) return
        const values = catalog.current.map((item) => (
            item.id === id ? { ...item, title, updatedAt: updatedAt ?? item.updatedAt } : item
        ))
        if (updatedAt) values.sort((a, b) => Date.parse(b.updatedAt) - Date.parse(a.updatedAt))
        catalog.current = values
        setSessions(values)
    }, [])

    useEffect(() => () => { request.current += 1 }, [])
    return { sessions, loading, error, refreshSessions, updateSessionTitle }
}
