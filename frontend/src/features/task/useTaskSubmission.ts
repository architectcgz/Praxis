import { useCallback, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { cancelTask, createTask, type AgentSnapshot, type ApiErrorCode } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

type TaskSubmissionContext = {
    agent: Pick<AgentSnapshot, 'state'> | null
    busy: boolean
    input: string
    sessionID: string
    refreshSession: () => Promise<void>
    getSelectedAgentID: () => string
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    setInput: Dispatch<SetStateAction<string>>
    viewRequestRef: RefObject<number>
}

/** 所有输入都先加入 Session 队列，前端不分配 Task、Request 或 Agent 执行身份。 */
export function useTaskSubmission({
    agent,
    busy,
    input,
    sessionID,
    refreshSession,
    getSelectedAgentID,
    setBusy,
    setError,
    setErrorCode,
    setInput,
    viewRequestRef,
}: TaskSubmissionContext) {
    const submitTask = useCallback(async () => {
        if (!sessionID || !input.trim() || busy) return
        const selectedAgentID = getSelectedAgentID()
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            // 创建不要求模型已就绪，暂不能调度时输入仍保留在队列中。
            const response = await createTask({ sessionId: sessionID, prompt: input })
            if (isCurrentView()) {
                setInput('')
                if (response.schedulingError) setError(`任务已加入队列，暂未开始执行：${response.schedulingError}`)
                await refreshSession()
            }
        } catch (err) {
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [busy, input, sessionID, refreshSession, getSelectedAgentID, viewRequestRef, setBusy, setError, setErrorCode, setInput])

    const cancelQueuedTask = useCallback(async (taskID: string) => {
        if (!sessionID || busy) return
        const viewRequestID = viewRequestRef.current
        const selectedAgentID = getSelectedAgentID()
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            await cancelTask(sessionID, taskID)
            if (isCurrentView()) await refreshSession()
        } catch (err) {
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [busy, sessionID, getSelectedAgentID, refreshSession, setBusy, setError, setErrorCode, viewRequestRef])

    return { canQueue: agent?.state === 'executing', submitTask, cancelQueuedTask }
}
