import { useCallback, useEffect, useRef, useState } from 'react'
import { listModels, type ModelOption } from '../../../api'
import { hasPrimaryModel } from './modelConfigHelpers'

/** 模型目录独立加载，配置查询失败不影响项目和会话数据。 */
export function useModelCatalog() {
    const [models, setModels] = useState<ModelOption[]>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState<unknown>()
    const request = useRef(0)

    const refreshModels = useCallback(async () => {
        const current = ++request.current
        setLoading(true)
        try {
            const values = await listModels()
            if (request.current !== current) return
            setModels(values)
            setError(undefined)
            return values
        } catch (reason) {
            if (request.current === current) setError(() => reason)
            throw reason
        } finally {
            if (request.current === current) setLoading(false)
        }
    }, [])

    const resetModels = useCallback(() => {
        request.current += 1
        setModels([])
        setLoading(false)
        setError(undefined)
    }, [])

    useEffect(() => () => { request.current += 1 }, [])

    return { models, primaryConfigured: hasPrimaryModel(models), loading, error, refreshModels, resetModels }
}
