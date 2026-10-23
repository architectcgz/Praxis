import { useCallback, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { stopAgent, pauseAgent, type AgentView, type ApiErrorCode } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import type { AgentStore } from './agentStore'

type AgentCommandContext = {
    agent: AgentView | null
    busy: boolean
    refreshAgent: (id: string) => Promise<void>
    getSelectedAgentID: () => string
    inputActions: Pick<AgentStore, 'stopAwaiting'>
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    viewRequestRef: RefObject<number>
}

/** 控制 Agent 当前执行，并只更新仍然有效的工作区视图。 */
export function useAgentCommands({
    agent,
    busy,
    refreshAgent,
    getSelectedAgentID,
    inputActions,
    setBusy,
    setError,
    setErrorCode,
    viewRequestRef,
}: AgentCommandContext) {
    const control = useCallback(async (kind: 'pause' | 'cancel') => {
        if (!agent?.currentExecutionId || busy) return
        const agentID = agent.ID
        const targetExecutionId = agent.currentExecutionId
        const selectedAgentID = getSelectedAgentID()
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        inputActions.stopAwaiting()
        setError('')
        setErrorCode(undefined)
        try {
            const commandId = crypto.randomUUID()
            const apply = kind === 'pause' ? pauseAgent : stopAgent
            const response = await apply({ commandId, agentId: agentID, targetExecutionId })
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

    return { control }
}
