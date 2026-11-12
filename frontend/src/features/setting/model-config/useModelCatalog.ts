import { useCallback, useEffect, useRef, useState } from 'react'
import { listModels, type ModelOption } from '../../../api'
import { hasModels } from './modelConfigHelpers'

/** 只接收已选定并完成加载的会话，读取其配置目录；保存全局配置不会更新这份目录。 */
export function useModelCatalog(sessionID: string) {
    const [models, setModels] = useState<ModelOption[]>([])
    const [loading, setLoading] = useState(false)
    const [error, setError] = useState<unknown>()
    const request = useRef(0)

    // 后台通知和页面刷新由本 Hook 展示错误；命令流程可要求抛错以中止后续步骤。
    const refreshModels = useCallback(async (throwOnError = true) => {
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
            if (throwOnError) throw reason
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
        if (sessionID) void refreshModels(false)
        return () => { request.current += 1 }
    }, [sessionID, refreshModels, resetModels])

    return { models, modelsConfigured: hasModels(models), loading, error, refreshModels, resetModels }
}
