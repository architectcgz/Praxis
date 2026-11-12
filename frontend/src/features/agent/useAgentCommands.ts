import { useCallback, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { stopAgent, type AgentView, type ApiErrorCode } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

type AgentCommandContext = {
    agent: AgentView | null
    busy: boolean
    getSelectedAgentID: () => string
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    viewRequestRef: RefObject<number>
}

/** 中断已选择的 Run，并只更新仍然有效的工作区视图。 */
export function useAgentCommands({
    agent,
    busy,
    getSelectedAgentID,
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
        setError('')
        setErrorCode(undefined)
        try {
            await stopAgent({ sessionId: agent.SessionID, branchId: agent.BranchID, agentId: agentID, runId: agent.currentRunId })
        } catch (err) {
            if (isCurrentView()) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        } finally {
            setBusy(false)
        }
    }, [agent, busy, getSelectedAgentID, viewRequestRef, setBusy, setError, setErrorCode])

    return { stop }
}
