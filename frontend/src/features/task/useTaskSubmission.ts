import { useCallback, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { API_ERROR_CODES, queueTask, sendInput, type AgentSnapshot, type ApiErrorCode, type SendInputRequest } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

type TaskSubmissionContext = {
    agent: Pick<AgentSnapshot, 'id' | 'definitionId' | 'state'> | null
    busy: boolean
    input: string
    reasoning: string
    refreshAgent: (id: string) => Promise<void>
    selectedModelID: string
    selectedProviderID: string
    getSelectedAgentID: () => string
    sessionID: string
    inputActions: {
        beginInput: (request: SendInputRequest) => void
        acceptInput: (requestId: string, executionId: string) => void
        rejectInput: (requestId: string) => void
    }
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    setInput: Dispatch<SetStateAction<string>>
    viewRequestRef: RefObject<number>
}

/** 任务提交统一处理立即执行与入队，通过回调通知消息展示，不依赖具体对话模块。 */
export function useTaskSubmission({
    agent,
    busy,
    input,
    reasoning,
    refreshAgent,
    selectedModelID,
    selectedProviderID,
    getSelectedAgentID,
    sessionID,
    inputActions,
    setBusy,
    setError,
    setErrorCode,
    setInput,
    viewRequestRef,
}: TaskSubmissionContext) {
    const supportsQueue = agent?.definitionId === 'primary'
    const canQueue = supportsQueue && agent.state === 'executing'
    const submitTask = useCallback(async () => {
        if (!sessionID || !input.trim() || busy) return
        if (!selectedProviderID.trim() || !selectedModelID.trim()) {
            setError('发送前请先配置 Provider 和 Model。')
            setErrorCode(API_ERROR_CODES.modelNotConfigured)
            return
        }
        const requestID = crypto.randomUUID()
        const taskID = crypto.randomUUID()
        const content = input
        const agentID = agent?.id || ''
        const selectedAgentID = getSelectedAgentID()
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        const request: SendInputRequest = {
            sessionId: sessionID,
            agentId: agentID,
            requestId: requestID,
            content,
            providerId: selectedProviderID,
            modelId: selectedModelID,
            reasoningLevel: reasoning,
        }
        setBusy(true)
        inputActions.beginInput(request)
        setError('')
        setErrorCode(undefined)
        try {
            const start = async () => {
                const response = await sendInput(request)
                return response.executionId
            }
            const enqueue = async () => {
                await queueTask({
                    taskId: taskID,
                    requestId: requestID,
                    agentId: agentID,
                    prompt: content,
                    providerId: selectedProviderID,
                    modelId: selectedModelID,
                    reasoningLevel: reasoning,
                })
                return ''
            }
            let executionID: string
            try {
                executionID = await (canQueue ? enqueue() : start())
            } catch (err) {
                // 快照与准入可能跨越执行交接；仅在后端明确拒绝准入时切换一次，不重试未知结果。
                const code = apiErrorCode(err)
                if (canQueue && code === API_ERROR_CODES.agentUnavailable) {
                    executionID = await start()
                } else if (!canQueue && supportsQueue && code === API_ERROR_CODES.agentExecuting) {
                    executionID = await enqueue()
                } else {
                    throw err
                }
            }
            inputActions.acceptInput(requestID, executionID)
            if (isCurrentView()) setInput('')
            await refreshAgent(agentID)
        } catch (err) {
            inputActions.rejectInput(requestID)
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [agent, busy, canQueue, supportsQueue, input, reasoning, sessionID, selectedModelID, selectedProviderID, refreshAgent, getSelectedAgentID, inputActions, viewRequestRef, setBusy, setError, setErrorCode, setInput])

    return { canQueue, submitTask }
}
