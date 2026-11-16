import { useSessionCommands } from '../session/useSessionCommands'
import { useWorkspaceData } from './useWorkspaceData'
export function useProjectWorkspace() {
    const data = useWorkspaceData()
    const commands = useSessionCommands({
        bridgeAvailable: data.bridgeAvailable,
        selectedSessionID: data.selectedSessionID,
        refreshCatalog: data.refreshCatalog,
        clearSelectedSession: data.clearSelectedSession,
        selectSession: data.selectSession,
        updateSessionTitle: data.updateSessionTitle,
    })
    return { ...data, ...commands, error: commands.error || data.error, errorCode: commands.error ? commands.errorCode : data.errorCode }
}
