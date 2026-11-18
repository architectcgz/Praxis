import { useCallback, useEffect, useRef, useState } from 'react'
import { stopRun } from '../../api'
import { readableError } from '../../shared/errors'

/** 按选中 Run 提交停止请求，执行状态由后端更新。 */
export function useRunControl(sessionId: string, runId: string | undefined) {
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const generation = useRef(0)
    const pending = useRef(false)
    useEffect(() => {
        generation.current += 1
        setError('')
        return () => { generation.current += 1 }
    }, [sessionId, runId])
    const stop = useCallback(async () => {
        if (!runId || pending.current) return
        const request = ++generation.current
        pending.current = true
        setBusy(true)
        setError('')
        try {
            await stopRun({ sessionId, runId })
        } catch (reason) {
            if (request === generation.current) setError(readableError(reason))
        } finally {
            pending.current = false
            setBusy(false)
        }
    }, [sessionId, runId])
    const clearError = useCallback(() => setError(''), [])
    return { stop, busy, error, clearError }
}
