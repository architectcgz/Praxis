import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { API_ERROR_CODES, isBindingAvailable, subscribeAgentEvents, type ApiErrorCode } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import { useAgentData } from '../agent/useAgentData'
import { useProjectData } from '../project/useProjectData'
import { useSessionData } from '../session/useSessionData'
import { useModelCatalog } from '../setting/model-config/useModelCatalog'
import { readRememberedSession, rememberSelectedSession } from './sessionPreference'

/** 工作区管理选择与视图生命周期，协调项目、会话、模型和 Agent 提供的数据能力。 */
export function useWorkspaceData() {
    const [selectedSessionID, setSelectedSessionID] = useState(readRememberedSession)
    const [error, setError] = useState('')
    const [errorCode, setErrorCode] = useState<ApiErrorCode | undefined>()
    const [catalogLoaded, setCatalogLoaded] = useState(false)
    const viewRequestRef = useRef(0)
    const catalogRequestRef = useRef(0)
    const selectedSessionIDRef = useRef(selectedSessionID)
    const errorCodeRef = useRef<ApiErrorCode | undefined>(undefined)
    const { projects, loading: projectsLoading, error: projectsError, refreshProjects, resetProjects } = useProjectData()
    const {
        sessions, session, catalogLoading, sessionLoading, catalogError, sessionError,
        getSessions, refreshSessions, loadSession, refreshSummary, updateSessionTitle, clearSession, resetSessions,
    } = useSessionData()
    const { models, primaryConfigured, loading: modelsLoading, error: modelsError, refreshModels, resetModels } = useModelCatalog()
    const { loadForSession, resetForSessionChange, ...agents } = useAgentData({ models, setError, setErrorCode, viewRequestRef })

    useLayoutEffect(() => {
        errorCodeRef.current = errorCode
    }, [errorCode])

    const clearError = useCallback(() => {
        setError('')
        setErrorCode(undefined)
    }, [])

    // 仅在模型目录更新后移除已解决的配置错误，不吞掉后来发生的发送失败。
    useEffect(() => {
        if (primaryConfigured && errorCodeRef.current === API_ERROR_CODES.modelNotConfigured) clearError()
    }, [models, primaryConfigured, clearError])

    const changeSelectedSession = useCallback((id: string) => {
        viewRequestRef.current += 1
        selectedSessionIDRef.current = id
        setSelectedSessionID(id)
        clearSession()
        resetForSessionChange()
        clearError()
    }, [clearSession, resetForSessionChange, clearError])

    const loadSelectedSession = useCallback(async (id: string, requestID: number) => {
        const isCurrent = () => viewRequestRef.current === requestID
        if (!id) {
            clearSession()
            resetForSessionChange()
            return
        }
        try {
            const value = await loadSession(id)
            if (isCurrent() && value) await loadForSession({ agents: value.agents }, requestID)
        } catch {
            if (!isCurrent()) return
            const catalog = getSessions()
            if (selectedSessionIDRef.current === id && !catalog.some((item) => item.id === id)) {
                changeSelectedSession(catalog[0]?.id || '')
            } else {
                resetForSessionChange()
            }
        }
    }, [loadSession, loadForSession, getSessions, changeSelectedSession, clearSession, resetForSessionChange])

    const refreshCatalog = useCallback(async (manual = false, throwOnError = false) => {
        const requestID = ++catalogRequestRef.current
        const isCurrent = () => catalogRequestRef.current === requestID
        if (!isBindingAvailable()) {
            resetProjects()
            resetSessions()
            resetModels()
            changeSelectedSession('')
            setCatalogLoaded(true)
            if (manual) {
                setError('桌面连接不可用，请在桌面应用中刷新工作区。')
                setErrorCode('binding_unavailable')
            }
            return
        }
        try {
            const projectCatalog = await refreshProjects()
            if (!isCurrent() || !projectCatalog) return
            const catalog = await refreshSessions(projectCatalog.map((project) => project.id))
            if (!isCurrent() || !catalog) return
            const selectedID = selectedSessionIDRef.current
            const nextID = catalog.some((item) => item.id === selectedID) ? selectedID : catalog[0]?.id || ''
            if (nextID !== selectedID) changeSelectedSession(nextID)
            if (errorCodeRef.current !== API_ERROR_CODES.modelNotConfigured) clearError()
        } catch (reason) {
            // 查询错误由各模块独立保留；命令调用可要求传播失败以避免误报成功。
            if (isCurrent() && throwOnError) throw reason
        } finally {
            if (isCurrent()) setCatalogLoaded(true)
        }
    }, [refreshProjects, refreshSessions, resetProjects, resetSessions, resetModels, changeSelectedSession, clearError])

    const refreshSession = useCallback(async () => {
        const requestID = ++viewRequestRef.current
        await loadSelectedSession(selectedSessionIDRef.current, requestID)
    }, [loadSelectedSession])

    const refresh = useCallback(async (manual = false) => {
        const currentID = selectedSessionIDRef.current
        const navigation = refreshCatalog(manual).then(() => {
            if (isBindingAvailable() && selectedSessionIDRef.current === currentID) return refreshSession()
        })
        // 模型刷新与导航刷新独立完成；任一失败都不阻止另一条链路提交成功结果。
        await Promise.allSettled([navigation, ...(isBindingAvailable() ? [refreshModels()] : [])])
    }, [refreshCatalog, refreshSession, refreshModels])

    useEffect(() => {
        void Promise.allSettled([refreshCatalog(), ...(isBindingAvailable() ? [refreshModels()] : [])])
        return () => {
            catalogRequestRef.current += 1
            viewRequestRef.current += 1
        }
    }, [refreshCatalog, refreshModels])

    useEffect(() => {
        if (!catalogLoaded) return
        const requestID = ++viewRequestRef.current
        void loadSelectedSession(selectedSessionID, requestID)
    }, [catalogLoaded, loadSelectedSession, selectedSessionID])

    useEffect(() => {
        rememberSelectedSession(selectedSessionID)
    }, [selectedSessionID])

    const selectSession = useCallback((id: string) => {
        if (id === selectedSessionIDRef.current) {
            if (error || sessionError) {
                clearError()
                void refreshSession()
            }
            return
        }
        changeSelectedSession(id)
    }, [error, sessionError, clearError, refreshSession, changeSelectedSession])

    const clearSelectedSession = useCallback((id: string) => {
        if (selectedSessionIDRef.current === id) changeSelectedSession('')
    }, [changeSelectedSession])

    useEffect(() => subscribeAgentEvents((event) => {
        if ((event.kind === 'execution_ended' || event.kind === 'request_canceled') && event.sessionId) {
            void refreshSummary(event.sessionId)
        }
    }), [refreshSummary])

    const dataError = sessionError ?? catalogError ?? projectsError ?? modelsError
    return {
        ...agents,
        bridgeAvailable: isBindingAvailable(),
        clearError,
        clearSelectedSession,
        error: error || (dataError === undefined ? '' : readableError(dataError)),
        errorCode: error ? errorCode : apiErrorCode(dataError),
        models,
        projects,
        refresh,
        refreshCatalog,
        refreshModels,
        refreshing: projectsLoading || catalogLoading || modelsLoading,
        workspaceLoading: !catalogLoaded,
        sessionLoading: sessionLoading || Boolean(catalogLoaded && selectedSessionID && !session && !sessionError),
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
