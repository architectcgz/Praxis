import { useCallback, useEffect, useRef, useState } from 'react'
import { cancelTask, createTask, pauseQueue, resumeQueue, subscribeAgentEvents } from '../../api'
import { readableError } from '../../shared/errors'

type Options = { sessionID: string; input: string; mainActive: boolean; setInput: (value: string) => void }
/** 操作拥有自己的请求状态；重试同一输入复用 SubmissionID。 */
export function useTaskSubmission({ sessionID, input, mainActive, setInput }: Options) {
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const pending = useRef(false)
    const generation = useRef(0)
    const submission = useRef<{ prompt: string; id: string } | null>(null)
    useEffect(() => () => { generation.current += 1 }, [])
    useEffect(() => subscribeAgentEvents(sessionID, (event) => {
        if (event.kind === 'task_queue_storage_failed') setError(`会话历史保存失败，队列已暂停：${event.data.message}`)
    }), [sessionID])
    const submitTask = useCallback(async () => {
        if (!input.trim() || pending.current) return false
        pending.current = true
        const request = ++generation.current
        setBusy(true); setError('')
        try {
            if (submission.current?.prompt !== input) submission.current = { prompt: input, id: crypto.randomUUID() }
            const value = await createTask({ sessionId: sessionID, submissionId: submission.current.id, prompt: input })
            if (request !== generation.current) return false
            submission.current = null
            setInput('')
            if (value.schedulingError) setError(`任务已接收，启动执行失败：${value.schedulingError}`)
            return true
        } catch (reason) { if (request === generation.current) setError(readableError(reason)); return false }
        finally { pending.current = false; if (request === generation.current) setBusy(false) }
    }, [input, sessionID, setInput])
    const cancelQueuedTask = useCallback(async (taskID: string) => {
        if (pending.current) return
        pending.current = true
        const request = ++generation.current
        setBusy(true); setError('')
        try { await cancelTask(sessionID, taskID) }
        catch (reason) { if (request === generation.current) setError(readableError(reason)) }
        finally { pending.current = false; if (request === generation.current) setBusy(false) }
    }, [sessionID])
    const changeQueue = useCallback(async (paused: boolean) => {
        if (pending.current) return
        pending.current = true
        const request = ++generation.current
        setBusy(true); setError('')
        try { await (paused ? pauseQueue(sessionID) : resumeQueue(sessionID)) }
        catch (reason) { if (request === generation.current) setError(readableError(reason)) }
        finally { pending.current = false; if (request === generation.current) setBusy(false) }
    }, [sessionID])
    const clearError = useCallback(() => setError(''), [])
    return { busy, error, clearError, canQueue: mainActive, submitTask, cancelQueuedTask, changeQueue }
}
