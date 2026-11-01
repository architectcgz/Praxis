import { useState } from 'react'
import { useSessionCommands } from '../session/useSessionCommands'
import { useWorkspaceData } from './useWorkspaceData'

export function useProjectWorkspace() {
    const data = useWorkspaceData()
    const [busy, setBusy] = useState(false)
    const commands = useSessionCommands({
        bridgeAvailable: data.bridgeAvailable,
        busy,
        refreshCatalog: data.refreshCatalog,
        clearSelectedSession: data.clearSelectedSession,
        selectSession: data.selectSession,
        updateSessionTitle: data.updateSessionTitle,
        setBusy,
        setError: data.setError,
        setErrorCode: data.setErrorCode,
        viewRequestRef: data.viewRequestRef,
    })
    return { ...data, ...commands, busy }
}
