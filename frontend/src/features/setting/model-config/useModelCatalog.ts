import { useCallback, useEffect, useRef, useState } from 'react'
import { listModels, type ModelOption } from '../../../api'
import { hasModels } from './modelConfigHelpers'

/** 读取当前会话已冻结的模型目录，保存全局配置不会更新这份目录。 */
export function useModelCatalog(sessionID: string) {
    const [models, setModels] = useState<ModelOption[]>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState<unknown>()
    const request = useRef(0)

    const refreshModels = useCallback(async () => {
        const current = ++request.current
        setLoading(true)
        try {
            const values = sessionID ? await listModels(sessionID) : []
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
    }, [sessionID])

    const resetModels = useCallback(() => {
        request.current += 1
        setModels([])
        setLoading(false)
        setError(undefined)
    }, [])

    useEffect(() => {
        resetModels()
        return () => { request.current += 1 }
    }, [sessionID, resetModels])

    return { models, modelsConfigured: hasModels(models), loading, error, refreshModels, resetModels }
}
