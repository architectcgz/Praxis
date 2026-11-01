import { useCallback, useEffect, useRef, useState } from 'react'
import { isBindingAvailable, subscribeAgentEvents, type ApiErrorCode } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import { useProjectData } from '../project/useProjectData'
import { useSessionCatalog } from '../session/useSessionCatalog'
import { readRememberedSession, rememberSelectedSession } from './sessionPreference'

/** 工作区只持有目录和已打开页面；各页面自行持有会话数据与事件订阅。 */
export function useWorkspaceData() {
    const [selectedSessionID, setSelectedSessionID] = useState(readRememberedSession)
    const [openedSessionIDs, setOpenedSessionIDs] = useState<string[]>([])
    const [catalogLoaded, setCatalogLoaded] = useState(false)
    const [error, setError] = useState('')
    const [errorCode, setErrorCode] = useState<ApiErrorCode | undefined>()
    const selectedID = useRef(selectedSessionID)
    const openedIDs = useRef<string[]>([])
    const viewRequestRef = useRef(0)
    const catalogRequest = useRef(0)
    const initialSelectionApplied = useRef(false)
    const { projects, loading: projectsLoading, error: projectsError, refreshProjects } = useProjectData()
    const { sessions, loading: sessionsLoading, error: sessionsError, refreshSessions, refreshSummary, updateSessionTitle } = useSessionCatalog()

    const clearError = useCallback(() => { setError(''); setErrorCode(undefined) }, [])
    const selectSession = useCallback((id: string) => {
        viewRequestRef.current += 1
        selectedID.current = id
        setSelectedSessionID(id)
        if (id && !openedIDs.current.includes(id)) {
            openedIDs.current = [...openedIDs.current, id]
            setOpenedSessionIDs(openedIDs.current)
        }
        clearError()
    }, [clearError])

    const clearSelectedSession = useCallback((id: string) => {
        const remaining = openedIDs.current.filter((item) => item !== id)
        openedIDs.current = remaining
        setOpenedSessionIDs(remaining)
        if (selectedID.current === id) selectSession(remaining.at(-1) || '')
    }, [selectSession])

    const refreshCatalog = useCallback(async (manual = false, throwOnError = false) => {
        const requestID = ++catalogRequest.current
        if (!isBindingAvailable()) {
            if (manual) { setError('桌面连接不可用，请在桌面应用中刷新工作区。'); setErrorCode('binding_unavailable') }
            setCatalogLoaded(true)
            return
        }
        try {
            const nextProjects = await refreshProjects()
            if (catalogRequest.current !== requestID || !nextProjects) return
            const values = await refreshSessions(nextProjects.map((item) => item.id))
            if (catalogRequest.current !== requestID || !values) return
            if (!initialSelectionApplied.current) {
                initialSelectionApplied.current = true
                const id = values.some((item) => item.id === selectedID.current) ? selectedID.current : values[0]?.id || ''
                selectSession(id)
            }
            clearError()
        } catch (reason) {
            if (catalogRequest.current === requestID && throwOnError) throw reason
        } finally {
            if (catalogRequest.current === requestID) setCatalogLoaded(true)
        }
    }, [refreshProjects, refreshSessions, selectSession, clearError])

    useEffect(() => {
        void refreshCatalog()
        return () => { catalogRequest.current += 1; viewRequestRef.current += 1 }
    }, [refreshCatalog])
    useEffect(() => { rememberSelectedSession(selectedSessionID) }, [selectedSessionID])
    useEffect(() => subscribeAgentEvents((event) => {
        if ((event.kind === 'execution_ended' || event.kind === 'request_canceled') && event.sessionId === event.agentId) {
            void refreshSummary(event.sessionId)
        }
    }), [refreshSummary])

    const dataError = sessionsError ?? projectsError
    return {
        projects, sessions, selectedSessionID, openedSessionIDs,
        selectSession, clearSelectedSession, updateSessionTitle, refreshCatalog,
        bridgeAvailable: isBindingAvailable(), workspaceLoading: !catalogLoaded,
        refreshing: projectsLoading || sessionsLoading,
        error: error || (dataError === undefined ? '' : readableError(dataError)),
        errorCode: error ? errorCode : apiErrorCode(dataError),
        clearError, setError, setErrorCode, viewRequestRef,
    }
}
