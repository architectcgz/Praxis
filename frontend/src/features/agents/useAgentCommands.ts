import { useCallback, type Dispatch, type RefObject, type SetStateAction } from 'react'
import {
    API_ERROR_CODES,
    cancelTurn,
    pauseAgent,
    sendInput,
    type AgentSnapshot,
    type ApiErrorCode,
} from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import type { PendingUserMessage } from './types'

type AgentCommandContext = {
    agent: AgentSnapshot | null
    busy: boolean
    input: string
    reasoning: string
    refreshAgent: (id: string) => Promise<void>
    selectedModelID: string
    selectedProviderID: string
    selectedAgentIDRef: RefObject<string>
    sessionID: string
    setAwaitingOutput: Dispatch<SetStateAction<boolean>>
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    setInput: Dispatch<SetStateAction<string>>
    setPendingUserMessages: Dispatch<SetStateAction<PendingUserMessage[]>>
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
    selectedAgentIDRef,
    sessionID,
    setAwaitingOutput,
    setBusy,
    setError,
    setErrorCode,
    setInput,
    setPendingUserMessages,
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
        const selectedAgentID = selectedAgentIDRef.current
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && selectedAgentIDRef.current === selectedAgentID
        setBusy(true)
        setAwaitingOutput(true)
        setPendingUserMessages((current) => [
            ...current,
            { requestId: requestID, turnId: '', sessionId: sessionID, agentId: agentID, content, at: new Date().toISOString() },
        ])
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
            setPendingUserMessages((current) => current.map((message) => (
                message.requestId === requestID ? { ...message, turnId: response.turnId } : message
            )))
            if (isCurrentView()) {
                setInput('')
            }
            await refreshAgent(agentID)
        } catch (err) {
            setPendingUserMessages((current) => current.filter((message) => message.requestId !== requestID))
            if (isCurrentView()) {
                setAwaitingOutput(false)
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [agent, busy, input, reasoning, sessionID, selectedModelID, selectedProviderID, refreshAgent, selectedAgentIDRef])

    const control = useCallback(async (kind: 'pause' | 'cancel') => {
        if (!agent?.currentTurnId || busy) {
            return
        }
        const agentID = agent.id
        const targetTurnId = agent.currentTurnId
        const selectedAgentID = selectedAgentIDRef.current
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && selectedAgentIDRef.current === selectedAgentID
        setBusy(true)
        setAwaitingOutput(false)
        setError('')
        setErrorCode(undefined)
        try {
            const commandId = crypto.randomUUID()
            const apply = kind === 'pause' ? pauseAgent : cancelTurn
            const response = await apply({ commandId, agentId: agentID, targetTurnId })
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
    }, [agent, busy, refreshAgent, selectedAgentIDRef])

    return { control, submitInput }
}
