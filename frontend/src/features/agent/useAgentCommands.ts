import { useCallback, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { stopAgent, type AgentView, type ApiErrorCode } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import type { AgentStore } from './agentStore'

type AgentCommandContext = {
    agent: AgentView | null
    busy: boolean
    getSelectedAgentID: () => string
    inputActions: Pick<AgentStore, 'stopAwaiting'>
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    viewRequestRef: RefObject<number>
}

/** 停止 Agent 当前执行，并只更新仍然有效的工作区视图。 */
export function useAgentCommands({
    agent,
    busy,
    getSelectedAgentID,
    inputActions,
    setBusy,
    setError,
    setErrorCode,
    viewRequestRef,
}: AgentCommandContext) {
    const stop = useCallback(async () => {
        if (!agent?.currentRunId || busy) return
        const agentID = agent.ID
        const selectedAgentID = getSelectedAgentID()
        const viewRequestID = viewRequestRef.current
        const isCurrentView = () => viewRequestRef.current === viewRequestID && getSelectedAgentID() === selectedAgentID
        setBusy(true)
        inputActions.stopAwaiting()
        setError('')
        setErrorCode(undefined)
        try {
            await stopAgent({ agentId: agentID })
        } catch (err) {
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [agent, busy, getSelectedAgentID, inputActions, viewRequestRef, setBusy, setError, setErrorCode])

    return { stop }
}
