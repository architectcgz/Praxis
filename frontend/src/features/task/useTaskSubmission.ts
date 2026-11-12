import { useCallback, useRef, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { cancelTask, createTask, type AgentView, type ApiErrorCode, type SessionSnapshot } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

type TaskSubmissionContext = {
    agent: Pick<AgentView, 'state'> | null
    busy: boolean
    input: string
    sessionID: string
    bindingID: string
    prepareSubmission: () => Promise<SessionSnapshot>
    bindSubmission: (bindingID: string) => void
    getSelectedAgentID: () => string
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    setInput: Dispatch<SetStateAction<string>>
    viewRequestRef: RefObject<number>
}

/** 首次发送才申请会话写入绑定；请求重试复用输入与绑定身份。 */
export function useTaskSubmission({
    agent,
    busy,
    input,
    sessionID,
    bindingID,
    prepareSubmission,
    bindSubmission,
    getSelectedAgentID,
    setBusy,
    setError,
    setErrorCode,
    setInput,
    viewRequestRef,
}: TaskSubmissionContext) {
    const submission = useRef<{ key: string; id: string; bindingId: string } | null>(null)
    const submitTask = useCallback(async (): Promise<boolean> => {
        if (!sessionID || !input.trim() || busy) return false
        const selectedAgentID = getSelectedAgentID()
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            const target = await prepareSubmission()
            if (!isCurrentView()) return false
            const key = JSON.stringify([target.id, target.branch.id, input])
            if (submission.current?.key !== key) submission.current = { key, id: crypto.randomUUID(), bindingId: target.bindingId || crypto.randomUUID() }
            // 创建不要求模型已就绪，暂不能调度时输入仍保留在队列中。
            const response = await createTask({ sessionId: target.id, bindingId: submission.current.bindingId, branchId: target.branch.id, submissionId: submission.current.id, prompt: input })
            if (isCurrentView()) {
                bindSubmission(response.bindingId)
                submission.current = null
                setInput('')
                if (response.schedulingError) setError(`任务已加入队列，暂未开始执行：${response.schedulingError}`)
                return true
            }
            return false
        } catch (err) {
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
        return false
    }, [busy, input, sessionID, prepareSubmission, bindSubmission, getSelectedAgentID, viewRequestRef, setBusy, setError, setErrorCode, setInput])

    const cancelQueuedTask = useCallback(async (taskID: string) => {
        if (!sessionID || busy) return
        const viewRequestID = viewRequestRef.current
        const selectedAgentID = getSelectedAgentID()
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            await cancelTask(sessionID, bindingID, taskID)
        } catch (err) {
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [busy, sessionID, bindingID, getSelectedAgentID, setBusy, setError, setErrorCode, viewRequestRef])

    return { canQueue: Boolean(bindingID) && agent?.state === 'executing', submitTask, cancelQueuedTask }
}
