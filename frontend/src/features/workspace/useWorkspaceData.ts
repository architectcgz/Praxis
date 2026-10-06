import { useCallback, useEffect, useRef, useState } from 'react'
import {
    API_ERROR_CODES,
    getSession,
    isBindingAvailable,
    listModels,
    listProjects,
    listSessions,
    type ApiErrorCode,
    type ModelOption,
    type ProjectSummary,
    type SessionSnapshot,
    type SessionSummary,
} from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import { useAgentData } from '../agents/useAgentData'
import { readRememberedSession, rememberSelectedSession } from './sessionPreference'

/** 协调项目与会话加载，并将 Agent 详情交给对应 feature 管理。 */
export function useWorkspaceData() {
    const [sessions, setSessions] = useState<SessionSummary[]>([])
    const [projects, setProjects] = useState<ProjectSummary[]>([])
    const [selectedSessionID, setSelectedSessionID] = useState(readRememberedSession)
    const [session, setSession] = useState<SessionSnapshot | null>(null)
    const [models, setModels] = useState<ModelOption[]>([])
    const [error, setError] = useState('')
    const [errorCode, setErrorCode] = useState<ApiErrorCode | undefined>()
    const [refreshing, setRefreshing] = useState(false)
    const [catalogLoaded, setCatalogLoaded] = useState(false)
    const viewRequestRef = useRef(0)
    const catalogRequestRef = useRef(0)
    const sessionCatalogRef = useRef(sessions)
    const selectedSessionIDRef = useRef(selectedSessionID)
    sessionCatalogRef.current = sessions
    selectedSessionIDRef.current = selectedSessionID
    const errorCodeRef = useRef<ApiErrorCode | undefined>(undefined)
    const refreshCatalogRef = useRef<(manual?: boolean) => Promise<void>>(async () => { })
    const {
        clearSessionData,
        loadForSession,
        resetForSessionChange,
        ...agents
    } = useAgentData({ models, setError, setErrorCode, viewRequestRef })
    errorCodeRef.current = errorCode

    const loadSelectedSession = useCallback(async (id: string, requestID: number) => {
        const isCurrentRequest = () => viewRequestRef.current === requestID
        if (!id) {
            setSession(null)
            clearSessionData()
            return
        }
        let nextSession: SessionSnapshot
        try {
            nextSession = await getSession(id)
        } catch (err) {
            if (!isCurrentRequest()) {
                return
            }
            if (selectedSessionIDRef.current === id && !sessionCatalogRef.current.some((item) => item.id === id)) {
                const nextSessionID = sessionCatalogRef.current[0]?.id || ''
                selectedSessionIDRef.current = nextSessionID
                setSelectedSessionID(nextSessionID)
                setError('')
                setErrorCode(undefined)
                return
            }
            clearSessionData()
            setSession(null)
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
            return
        }
        if (!isCurrentRequest()) {
            return
        }
        setSession(nextSession)
        await loadForSession({ agents: nextSession.agents }, requestID)
    }, [clearSessionData, loadForSession, setError, setErrorCode, viewRequestRef])

    const refreshCatalog = useCallback(async (manual = false, throwOnError = false) => {
        const requestID = ++catalogRequestRef.current
        const isCurrentRequest = () => catalogRequestRef.current === requestID
        if (!isBindingAvailable()) {
            setSessions([])
            sessionCatalogRef.current = []
            setProjects([])
            setSession(null)
            selectedSessionIDRef.current = ''
            setSelectedSessionID('')
            setCatalogLoaded(true)
            viewRequestRef.current += 1
            clearSessionData()
            setModels([])
            setRefreshing(false)
            if (manual) {
                setError('桌面连接不可用，请在桌面应用中刷新工作区。')
                setErrorCode('binding_unavailable')
            }
            return
        }

        setRefreshing(true)
        try {
            const [projectCatalog, configuredModels] = await Promise.all([listProjects(), listModels()])
            const sessionCatalogs = await Promise.all(projectCatalog.map((project) => listSessions(project.id)))
            const catalog = sessionCatalogs
                .flat()
                .sort((left, right) => Date.parse(right.updatedAt) - Date.parse(left.updatedAt))
            if (!isCurrentRequest()) {
                return
            }
            setModels(configuredModels)
            const primaryConfigured = configuredModels.some((model) => model.assignedAgentDefinitions.includes('primary'))
            if (errorCodeRef.current !== API_ERROR_CODES.modelNotConfigured || primaryConfigured) {
                setError('')
                setErrorCode(undefined)
            }
            setSessions(catalog)
            sessionCatalogRef.current = catalog
            setProjects(projectCatalog)
            const firstSessionID = catalog[0]?.id || ''
            const selectedID = selectedSessionIDRef.current
            const nextSessionID = catalog.some((item) => item.id === selectedID) ? selectedID : firstSessionID
            if (nextSessionID !== selectedID) {
                if (selectedID) {
                    viewRequestRef.current += 1
                    setSession(null)
                    resetForSessionChange()
                }
                selectedSessionIDRef.current = nextSessionID
                setSelectedSessionID(nextSessionID)
            }
            setCatalogLoaded(true)
        } catch (err) {
            if (!isCurrentRequest()) {
                return
            }
            if (throwOnError) throw err
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
        } finally {
            if (isCurrentRequest()) {
                setRefreshing(false)
            }
        }
    }, [clearSessionData, resetForSessionChange, setError, setErrorCode, viewRequestRef])

    const refreshSession = useCallback(async () => {
        const requestID = ++viewRequestRef.current
        await loadSelectedSession(selectedSessionIDRef.current, requestID)
    }, [loadSelectedSession, viewRequestRef])

    refreshCatalogRef.current = refreshCatalog

    const refresh = useCallback(async (manual = false) => {
        const currentSessionID = selectedSessionIDRef.current
        await refreshCatalog(manual)
        if (isBindingAvailable() && selectedSessionIDRef.current === currentSessionID) {
            await refreshSession()
        }
    }, [refreshCatalog, refreshSession])

    useEffect(() => {
        void refreshCatalog()
    }, [refreshCatalog])

    useEffect(() => {
        if (!catalogLoaded) {
            return
        }
        const requestID = ++viewRequestRef.current
        void loadSelectedSession(selectedSessionID, requestID)
    }, [catalogLoaded, loadSelectedSession, selectedSessionID])

    useEffect(() => {
        rememberSelectedSession(selectedSessionID)
    }, [selectedSessionID])

    const selectSession = useCallback((id: string) => {
        viewRequestRef.current += 1
        selectedSessionIDRef.current = id
        setError('')
        setErrorCode(undefined)
        setSelectedSessionID(id)
        setSession(null)
        resetForSessionChange()
    }, [resetForSessionChange, setError, setErrorCode, viewRequestRef])

    const clearError = useCallback(() => {
        setError('')
        setErrorCode(undefined)
    }, [])

    const updateSessionTitle = useCallback((id: string, title: string) => {
        setSession((current) => current?.id === id ? { ...current, title } : current)
    }, [])

    return {
        ...agents,
        bridgeAvailable: isBindingAvailable(),
        clearError,
        error,
        errorCode,
        models,
        projects,
        refresh,
        refreshCatalog,
        refreshCatalogRef,
        refreshing,
        workspaceLoading: !catalogLoaded && (refreshing || !error),
        sessionLoading: catalogLoaded && Boolean(selectedSessionID && !session && !error),
        selectedSessionID,
        selectSession,
        session,
        sessions,
        updateSessionTitle,
        setError,
        setErrorCode,
        viewRequestRef,
    }
}
