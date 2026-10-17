import { useState } from 'react'
import { useAgentCommands } from '../agent/useAgentCommands'
import { useSessionCommands } from '../session/useSessionCommands'
import { useWorkspaceData } from './useWorkspaceData'
import { useWorkspaceCommands } from './useWorkspaceCommands'

export function useProjectWorkspace() {
    const workspaceData = useWorkspaceData()
    const [busy, setBusy] = useState(false)
    const {
        setError,
        setErrorCode,
        setInput,
        viewRequestRef,
        ...workspace
    } = workspaceData
    const sessionCommands = useSessionCommands({
        bridgeAvailable: workspace.bridgeAvailable,
        busy,
        refreshCatalog: workspace.refreshCatalog,
        clearSelectedSession: workspace.clearSelectedSession,
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
        getSelectedAgentID: workspace.getSelectedAgentID,
        sessionID: workspace.session?.id || '',
        inputActions: workspace.inputActions,
        setBusy,
        setError,
        setErrorCode,
        setInput,
        viewRequestRef,
    })

    const { runCommand, commandNotice, clearCommandNotice } = useWorkspaceCommands({
        session: workspace.session,
        bridgeAvailable: workspace.bridgeAvailable,
        busy,
        createNewSession: sessionCommands.createNewSession,
        refreshCatalog: workspace.refreshCatalog,
        viewRequestRef,
        selectedAgentID: workspace.selectedAgentID,
        getSelectedAgentID: workspace.getSelectedAgentID,
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
