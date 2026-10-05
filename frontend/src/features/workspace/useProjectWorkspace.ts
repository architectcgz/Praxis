import { useState } from 'react'
import { useAgentCommands } from '../agents/useAgentCommands'
import { useSessionCommands } from '../sessions/useSessionCommands'
import { useWorkspaceData } from './useWorkspaceData'
import { useWorkspaceCommands } from './useWorkspaceCommands'

export function useProjectWorkspace() {
    const workspaceData = useWorkspaceData()
    const [busy, setBusy] = useState(false)
    const {
        refreshCatalogRef,
        setAwaitingOutput,
        setError,
        setErrorCode,
        setInput,
        setPendingUserMessages,
        viewRequestRef,
        ...workspace
    } = workspaceData
    const sessionCommands = useSessionCommands({
        bridgeAvailable: workspace.bridgeAvailable,
        busy,
        refreshCatalogRef,
        selectedSessionID: workspace.selectedSessionID,
        selectSession: workspace.selectSession,
        setBusy,
        setError,
        setErrorCode,
        updateSessionTitle: workspace.updateSessionTitle,
        viewRequestRef,
    })
    const agentCommands = useAgentCommands({
        agent: workspace.agent,
        busy,
        input: workspace.input,
        reasoning: workspace.reasoning,
        refreshAgent: workspace.refreshAgent,
        selectedModelID: workspace.selectedModelID,
        selectedProviderID: workspace.selectedProviderID,
        selectedAgentIDRef: workspace.selectedAgentIDRef,
        sessionID: workspace.session?.id || '',
        setAwaitingOutput,
        setBusy,
        setError,
        setErrorCode,
        setInput,
        setPendingUserMessages,
        viewRequestRef,
    })

    const { runCommand, commandNotice, clearCommandNotice } = useWorkspaceCommands({
        session: workspace.session,
        bridgeAvailable: workspace.bridgeAvailable,
        busy,
        createNewSession: sessionCommands.createNewSession,
        refreshCatalog: workspace.refreshCatalog,
        viewRequestRef,
        selectedAgentIDRef: workspace.selectedAgentIDRef,
        setBusy,
        setError,
        setErrorCode,
        setInput,
    })

    const submitInput = () => {
        const content = workspace.input.trim()
        if (content.startsWith('/')) {
            void runCommand(content)
        } else {
            void agentCommands.submitInput()
        }
    }

    return {
        ...workspace,
        ...sessionCommands,
        ...agentCommands,
        submitInput,
        runCommand,
        commandNotice,
        clearCommandNotice,
        busy,
        setInput,
    }
}
