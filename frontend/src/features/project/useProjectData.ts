import { useCallback, useEffect, useRef, useState } from 'react'
import { listProjects, type ProjectSummary } from '../../api'

/** 项目目录保留最近成功结果，查询失败和过期响应不会清空已有导航。 */
export function useProjectData() {
    const [projects, setProjects] = useState<ProjectSummary[]>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState<unknown>()
    const request = useRef(0)

    const refreshProjects = useCallback(async () => {
        const current = ++request.current
        setLoading(true)
        try {
            const values = await listProjects()
            if (request.current !== current) return
            setProjects(values)
            setError(undefined)
            return values
        } catch (reason) {
            if (request.current === current) setError(() => reason)
            throw reason
        } finally {
            if (request.current === current) setLoading(false)
        }
    }, [])

    const resetProjects = useCallback(() => {
        request.current += 1
        setProjects([])
        setLoading(false)
        setError(undefined)
    }, [])

    useEffect(() => () => { request.current += 1 }, [])

    return { projects, loading, error, refreshProjects, resetProjects }
}
