import { useCallback, useRef, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { cancelTask, createTask, getSession, type AgentView, type ApiErrorCode, type SessionSnapshot } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

type TaskSubmissionContext = {
    agent: Pick<AgentView, 'state'> | null
    busy: boolean
    input: string
    sessionID: string
    bindingID: string
    prepareSubmission: () => Promise<SessionSnapshot>
    applySession: (session: SessionSnapshot) => Promise<void>
    refreshSession: () => Promise<void>
    getSelectedAgentID: () => string
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    setInput: Dispatch<SetStateAction<string>>
    viewRequestRef: RefObject<number>
}

/** 首次发送才申请写入绑定；自动分叉后按后端返回的实际分支更新页面。 */
export function useTaskSubmission({
    agent,
    busy,
    input,
    sessionID,
    bindingID,
    prepareSubmission,
    applySession,
    refreshSession,
    getSelectedAgentID,
    setBusy,
    setError,
    setErrorCode,
    setInput,
    viewRequestRef,
}: TaskSubmissionContext) {
    const submission = useRef<{ key: string; id: string; bindingId: string } | null>(null)
    const submitTask = useCallback(async () => {
        if (!sessionID || !input.trim() || busy) return
        const selectedAgentID = getSelectedAgentID()
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            const target = await prepareSubmission()
            if (!isCurrentView()) return
            const key = JSON.stringify([target.id, target.branch.id, input])
            if (submission.current?.key !== key) submission.current = { key, id: crypto.randomUUID(), bindingId: target.bindingId || crypto.randomUUID() }
            // 创建不要求模型已就绪，暂不能调度时输入仍保留在队列中。
            const response = await createTask({ sessionId: target.id, bindingId: submission.current.bindingId, branchId: target.branch.id, submissionId: submission.current.id, prompt: input })
            if (isCurrentView()) {
                const next = await getSession(target.id, response.branchId, response.bindingId)
                if (!isCurrentView()) return
                await applySession(next)
                submission.current = null
                setInput('')
                if (response.schedulingError) setError(`任务已加入队列，暂未开始执行：${response.schedulingError}`)
            }
        } catch (err) {
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [busy, input, sessionID, prepareSubmission, applySession, getSelectedAgentID, viewRequestRef, setBusy, setError, setErrorCode, setInput])

    const cancelQueuedTask = useCallback(async (taskID: string) => {
        if (!sessionID || busy) return
        const viewRequestID = viewRequestRef.current
        const selectedAgentID = getSelectedAgentID()
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            await cancelTask(bindingID, taskID)
            if (isCurrentView()) await refreshSession()
        } catch (err) {
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [busy, sessionID, bindingID, getSelectedAgentID, refreshSession, setBusy, setError, setErrorCode, viewRequestRef])

    return { canQueue: Boolean(bindingID) && agent?.state === 'executing', submitTask, cancelQueuedTask }
}
