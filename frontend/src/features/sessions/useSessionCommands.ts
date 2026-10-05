import { useCallback, type Dispatch, type RefObject, type SetStateAction } from 'react'
import {
    createSession,
    deleteSession as deleteSessionAPI,
    renameSession as renameSessionAPI,
    type ApiErrorCode,
} from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

type SessionCommandContext = {
    bridgeAvailable: boolean
    busy: boolean
    refreshCatalogRef: RefObject<(manual?: boolean) => Promise<void>>
    selectedSessionID: string
    selectSession: (id: string) => void
    setBusy: Dispatch<SetStateAction<boolean>>
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    updateSessionTitle: (id: string, title: string) => void
    viewRequestRef: RefObject<number>
}

/** 创建或管理 Session，并在完成后同步工作区选中状态。 */
export function useSessionCommands({
    bridgeAvailable,
    busy,
    refreshCatalogRef,
    selectedSessionID,
    selectSession,
    setBusy,
    setError,
    setErrorCode,
    updateSessionTitle,
    viewRequestRef,
}: SessionCommandContext) {
    const createNewSession = useCallback(async (projectID: string, workspaceID: string) => {
        if (busy || !projectID || !workspaceID || !bridgeAvailable) {
            return false
        }
        const viewRequestID = viewRequestRef.current
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            const created = await createSession({
                sessionId: crypto.randomUUID(),
                agentId: crypto.randomUUID(),
                projectId: projectID,
                workspaceId: workspaceID,
                agentDefinitionId: 'primary',
                requestId: crypto.randomUUID(),
            })
            await refreshCatalogRef.current()
            if (viewRequestRef.current === viewRequestID) {
                selectSession(created.sessionId)
            }
            return true
        } catch (err) {
            if (viewRequestRef.current === viewRequestID) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
            return false
        } finally {
            setBusy(false)
        }
    }, [bridgeAvailable, busy, refreshCatalogRef, selectSession])

    const deleteSession = useCallback(async (sessionID: string) => {
        if (busy || !sessionID || !bridgeAvailable) {
            return false
        }
        const viewRequestID = viewRequestRef.current
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            await deleteSessionAPI(sessionID)
            if (sessionID === selectedSessionID) {
                selectSession('')
            }
            await refreshCatalogRef.current()
            return true
        } catch (err) {
            if (viewRequestRef.current === viewRequestID) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
            return false
        } finally {
            setBusy(false)
        }
    }, [bridgeAvailable, busy, refreshCatalogRef, selectedSessionID, selectSession])

    const renameSession = useCallback(async (sessionID: string, title: string) => {
        if (busy || !sessionID || !title.trim() || !bridgeAvailable) {
            return false
        }
        const viewRequestID = viewRequestRef.current
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            await renameSessionAPI(sessionID, title.trim())
            if (viewRequestRef.current === viewRequestID) updateSessionTitle(sessionID, title.trim())
            await refreshCatalogRef.current()
            return true
        } catch (err) {
            if (viewRequestRef.current === viewRequestID) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
            return false
        } finally {
            setBusy(false)
        }
    }, [bridgeAvailable, busy, refreshCatalogRef, updateSessionTitle, viewRequestRef])

    return { createNewSession, deleteSession, renameSession }
}
