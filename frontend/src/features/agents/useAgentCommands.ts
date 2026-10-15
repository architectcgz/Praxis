import { useCallback, type Dispatch, type RefObject, type SetStateAction } from 'react'
import {
    API_ERROR_CODES,
    cancelTask,
    pauseAgent,
    sendInput,
    type AgentSnapshot,
    type ApiErrorCode,
} from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import type { AgentStore } from './agentStore'

type AgentCommandContext = {
    agent: AgentSnapshot | null
    busy: boolean
    input: string
    reasoning: string
    refreshAgent: (id: string) => Promise<void>
    selectedModelID: string
    selectedProviderID: string
    getSelectedAgentID: () => string
    sessionID: string
    inputActions: Pick<AgentStore, 'beginInput' | 'acceptInput' | 'rejectInput' | 'stopAwaiting'>
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    setInput: Dispatch<SetStateAction<string>>
    viewRequestRef: RefObject<number>
}

/** 执行 Agent 输入与控制命令，并只更新仍然有效的工作区视图。 */
export function useAgentCommands({
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
}: AgentCommandContext) {
    const submitInput = useCallback(async () => {
        if (!sessionID || !input.trim() || busy) {
            return
        }
        if (!selectedProviderID.trim() || !selectedModelID.trim()) {
            setError('发送前请先配置 Provider 和 Model。')
            setErrorCode(API_ERROR_CODES.modelNotConfigured)
            return
        }
        const requestID = crypto.randomUUID()
        const content = input
        const agentID = agent?.id || ''
        const selectedAgentID = getSelectedAgentID()
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        inputActions.beginInput({ requestId: requestID, taskId: '', sessionId: sessionID, agentId: agentID, content, at: new Date().toISOString() })
        setError('')
        setErrorCode(undefined)
        try {
            const response = await sendInput({
                sessionId: sessionID,
                agentId: agentID,
                requestId: requestID,
                content,
                providerId: selectedProviderID,
                modelId: selectedModelID,
                reasoningLevel: reasoning,
            })
            inputActions.acceptInput(requestID, response.taskId)
            if (isCurrentView()) {
                setInput('')
            }
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
    }, [agent, busy, input, reasoning, sessionID, selectedModelID, selectedProviderID, refreshAgent, getSelectedAgentID, inputActions, viewRequestRef, setBusy, setError, setErrorCode, setInput])

    const control = useCallback(async (kind: 'pause' | 'cancel') => {
        if (!agent?.currentTaskId || busy) {
            return
        }
        const agentID = agent.id
        const targetTaskId = agent.currentTaskId
        const selectedAgentID = getSelectedAgentID()
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        inputActions.stopAwaiting()
        setError('')
        setErrorCode(undefined)
        try {
            const commandId = crypto.randomUUID()
            const apply = kind === 'pause' ? pauseAgent : cancelTask
            const response = await apply({ commandId, agentId: agentID, targetTaskId })
            if (response.cancellationError) throw new Error('取消通知失败，请重试。')
            await refreshAgent(agentID)
        } catch (err) {
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [agent, busy, refreshAgent, getSelectedAgentID, inputActions, viewRequestRef, setBusy, setError, setErrorCode])

    return { control, submitInput }
}
