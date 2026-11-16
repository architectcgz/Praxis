import { useCallback, useEffect, useRef, useState } from 'react'
import {
    createSession,
    deleteSession as deleteSessionAPI,
    renameSession as renameSessionAPI,
    revealSessionFile as revealSessionFileAPI,
    type ApiErrorCode,
} from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'

type SessionCommandContext = {
    bridgeAvailable: boolean
    selectedSessionID: string
    refreshCatalog: (manual?: boolean) => Promise<void>
    clearSelectedSession: (id: string) => void
    selectSession: (id: string) => void
    updateSessionTitle: (id: string, title: string) => void
}

/** 创建或管理 Session，并在完成后同步工作区选中状态。 */
export function useSessionCommands({
    bridgeAvailable,
    selectedSessionID,
    refreshCatalog,
    clearSelectedSession,
    selectSession,
    updateSessionTitle,
}: SessionCommandContext) {
    const [busy, setBusy] = useState(false)
    const [error, setError] = useState('')
    const [errorCode, setErrorCode] = useState<ApiErrorCode | undefined>()
    const viewRequestRef = useRef(0)
    const pending = useRef(false)
    useEffect(() => { viewRequestRef.current += 1; return () => { viewRequestRef.current += 1 } }, [selectedSessionID])
    const createNewSession = useCallback(async (projectID: string) => {
        if (pending.current || !projectID || !bridgeAvailable) {
            return false
        }
        const viewRequestID = viewRequestRef.current
        pending.current = true
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            const created = await createSession({
                sessionId: crypto.randomUUID(),
                projectId: projectID,
                requestId: crypto.randomUUID(),
            })
            await refreshCatalog()
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
            pending.current = false
            setBusy(false)
        }
    }, [bridgeAvailable, refreshCatalog, selectSession, setBusy, setError, setErrorCode, viewRequestRef])

    const deleteSession = useCallback(async (sessionID: string) => {
        if (pending.current || !sessionID || !bridgeAvailable) {
            return false
        }
        const viewRequestID = viewRequestRef.current
        pending.current = true
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            await deleteSessionAPI(sessionID)
            clearSelectedSession(sessionID)
            await refreshCatalog()
            return true
        } catch (err) {
            if (viewRequestRef.current === viewRequestID) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
            return false
        } finally {
            pending.current = false
            setBusy(false)
        }
    }, [bridgeAvailable, refreshCatalog, clearSelectedSession, setBusy, setError, setErrorCode, viewRequestRef])

    const renameSession = useCallback(async (sessionID: string, title: string) => {
        if (pending.current || !sessionID || !title.trim() || !bridgeAvailable) {
            return false
        }
        const viewRequestID = viewRequestRef.current
        pending.current = true
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            await renameSessionAPI(sessionID, title.trim())
            if (viewRequestRef.current === viewRequestID) updateSessionTitle(sessionID, title.trim())
            await refreshCatalog()
            return true
        } catch (err) {
            if (viewRequestRef.current === viewRequestID) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
            return false
        } finally {
            pending.current = false
            setBusy(false)
        }
    }, [bridgeAvailable, refreshCatalog, updateSessionTitle, setBusy, setError, setErrorCode, viewRequestRef])

    const revealSessionFile = useCallback(async (sessionID: string) => {
        if (pending.current || !sessionID || !bridgeAvailable) return false
        const viewRequestID = viewRequestRef.current
        pending.current = true
        setBusy(true)
        setError('')
        setErrorCode(undefined)
        try {
            await revealSessionFileAPI(sessionID)
            return true
        } catch (err) {
            if (viewRequestRef.current === viewRequestID) {
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
            return false
        } finally {
            pending.current = false
            setBusy(false)
        }
    }, [bridgeAvailable, setBusy, setError, setErrorCode, viewRequestRef])

    return { createNewSession, deleteSession, renameSession, revealSessionFile, busy, error, errorCode }
}
