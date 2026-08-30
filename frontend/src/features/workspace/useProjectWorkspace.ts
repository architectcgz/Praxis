import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import {
    API_ERROR_CODES,
    AgentHistoryItem,
    AgentOutputEvent,
    AgentSnapshot,
    HealthSnapshot,
    ModelOption,
    ProjectSummary,
    SessionSnapshot,
    SessionSummary,
    StartupIssue,
    getAgent,
    getSession,
    isBindingAvailable,
    listAgentHistory,
    listModels,
    listProjects,
    listSessions,
    loadReadiness,
    requestControl,
    sendInput,
    subscribeAgentOutput,
    isApiErrorCode,
    type ApiErrorCode,
} from '../../api'
import { readableError } from '../../shared/errors'
import { readRememberedSession, rememberSelectedSession } from './sessionPreference'
import { StreamingOutput } from '../sessions/types'

const runtime = {
    sandboxMode: 'read_only',
    approvalMode: 'always_ask',
    revision: 'ui-v1',
}

type StreamingOutputs = Record<string, StreamingOutput>

function clearDurableStreamingOutput(outputs: StreamingOutputs, agentID: string, history: AgentHistoryItem[]): StreamingOutputs {
    const output = outputs[agentID]
    if (!output) {
        return outputs
    }
    const persisted = history.some((item) => (
        item.message?.role === 'assistant' && item.message.executionId === output.executionId
    ) || item.execution?.id === output.executionId)
    if (!persisted) {
        return outputs
    }
    const remaining = { ...outputs }
    delete remaining[agentID]
    return remaining
}

export function useProjectWorkspace() {
    const [health, setHealth] = useState<HealthSnapshot>({ ready: false })
    const [sessions, setSessions] = useState<SessionSummary[]>([])
    const [projects, setProjects] = useState<ProjectSummary[]>([])
    const [selectedSessionID, setSelectedSessionID] = useState(readRememberedSession)
    const [session, setSession] = useState<SessionSnapshot | null>(null)
    const [selectedAgentID, setSelectedAgentID] = useState('')
    const [agent, setAgent] = useState<AgentSnapshot | null>(null)
    const [agentLoading, setAgentLoading] = useState(false)
    const [history, setHistory] = useState<AgentHistoryItem[]>([])
    const [streamingOutputs, setStreamingOutputs] = useState<StreamingOutputs>({})
    const [awaitingOutput, setAwaitingOutput] = useState(false)
    const [models, setModels] = useState<ModelOption[]>([])
    const [selectedProviderID, setSelectedProviderID] = useState('')
    const [selectedModelID, setSelectedModelID] = useState('')
    const [reasoning, setReasoning] = useState('')
    const [input, setInput] = useState('')
    const [error, setError] = useState('')
    const [errorCode, setErrorCode] = useState<ApiErrorCode | undefined>()
    const [busy, setBusy] = useState(false)
    const [refreshing, setRefreshing] = useState(false)
    const viewRequestRef = useRef(0)
    const errorCodeRef = useRef<ApiErrorCode | undefined>(undefined)
    const refreshRef = useRef<(manual?: boolean) => Promise<void>>(async () => { })
    const streamingOutput = agent ? streamingOutputs[agent.id] || null : null
    errorCodeRef.current = errorCode

    const refresh = useCallback(async (manual = false) => {
        const requestID = ++viewRequestRef.current
        const isCurrentRequest = () => viewRequestRef.current === requestID
        if (!isBindingAvailable()) {
            setHealth({ ready: false })
            setSessions([])
            setProjects([])
            setSession(null)
            setAgent(null)
            setAgentLoading(false)
            setHistory([])
            setStreamingOutputs({})
            setModels([])
            if (manual) {
                setError('Wails bridge is unavailable. Run the desktop app to refresh workspace data.')
                setErrorCode('binding_unavailable')
            }
            return
        }

        setRefreshing(true)
        let catalogSessions: SessionSummary[] = []
        try {
            const nextHealth = await loadReadiness()
            if (!isCurrentRequest()) {
                return
            }
            setHealth(nextHealth)
            if (!nextHealth.ready) {
                if (nextHealth.issue) {
                    setError(startupIssueMessage(nextHealth.issue))
                    setErrorCode(isApiErrorCode(nextHealth.issue.code) ? nextHealth.issue.code : undefined)
                } else if (manual) {
                    setError('Praxis is still starting. Try refreshing again in a moment.')
                    setErrorCode('orchestration_not_ready')
                }
                return
            }

            const [projectCatalog, configuredModels] = await Promise.all([listProjects(), listModels()])
            const sessionCatalogs = await Promise.all(projectCatalog.map((project) => listSessions(project.id)))
            const catalog = sessionCatalogs
                .flat()
                .sort((left, right) => Date.parse(right.updatedAt) - Date.parse(left.updatedAt))
            if (!isCurrentRequest()) {
                return
            }
            catalogSessions = catalog
            setModels(configuredModels)
            const primaryConfigured = configuredModels.some((model) => model.defaultProfiles.includes('primary'))
            if (errorCodeRef.current !== API_ERROR_CODES.modelNotConfigured || primaryConfigured) {
                setError('')
                setErrorCode(undefined)
            }
            setSessions(catalogSessions)
            setProjects(projectCatalog)
            const firstSessionID = catalogSessions[0]?.id || ''
            const nextSessionID = selectedSessionID || firstSessionID
            if (nextSessionID !== selectedSessionID) {
                setSelectedSessionID(nextSessionID)
                return
            }
            if (!nextSessionID) {
                setSession(null)
                setAgent(null)
                setAgentLoading(false)
                setHistory([])
                setStreamingOutputs({})
                return
            }

            const nextSession = await getSession(nextSessionID)
            if (!isCurrentRequest()) {
                return
            }
            const nextAgentID = nextSession.agents.some((item) => item.id === selectedAgentID)
                ? selectedAgentID
                : nextSession.agents[0]?.id || ''
            if (!nextAgentID) {
                setSession(nextSession)
                setSelectedAgentID('')
                setAgent(null)
                setAgentLoading(false)
                setHistory([])
                setStreamingOutputs({})
                return
            }
            const [nextAgent, nextHistory] = await Promise.all([
                getAgent(nextAgentID),
                listAgentHistory(nextAgentID),
            ])
            if (!isCurrentRequest()) {
                return
            }
            setSession(nextSession)
            setSelectedAgentID(nextAgentID)
            setAgent(nextAgent)
            setAgentLoading(false)
            setHistory(nextHistory)
            setStreamingOutputs((current) => clearDurableStreamingOutput(current, nextAgentID, nextHistory))
        } catch (err) {
            if (!isCurrentRequest()) {
                return
            }
            setAgent(null)
            setAgentLoading(false)
            setHistory([])
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
            if (selectedSessionID && !catalogSessions.some((item) => item.id === selectedSessionID)) {
                setSelectedSessionID(catalogSessions[0]?.id || '')
            }
        } finally {
            if (isCurrentRequest()) {
                setRefreshing(false)
            }
        }
    }, [selectedAgentID, selectedSessionID])

    refreshRef.current = refresh

    useEffect(() => {
        void refresh()
        const timer = window.setInterval(() => void refresh(), 10000)
        return () => window.clearInterval(timer)
    }, [refresh])

    useEffect(() => subscribeAgentOutput((event: AgentOutputEvent) => {
        if (event.kind === 'settled') {
            void refreshRef.current()
            return
        }
        setStreamingOutputs((current) => {
            const output = current[event.agentId]
            if (output?.executionId === event.executionId) {
                return { ...current, [event.agentId]: { ...output, content: output.content + event.text } }
            }
            return { ...current, [event.agentId]: { executionId: event.executionId, content: event.text } }
        })
    }), [])

    useEffect(() => {
        if (streamingOutput || agent?.state !== 'executing') {
            setAwaitingOutput(false)
        }
    }, [agent?.state, streamingOutput])

    useEffect(() => {
        rememberSelectedSession(selectedSessionID)
    }, [selectedSessionID])

    const modelCatalogKey = models.map((model) => [
        model.providerId,
        model.modelId,
        model.reasoning.supported,
        model.reasoning.levels.join(','),
        model.reasoning.default,
        model.defaultProfiles.join(','),
    ].join(':')).join('|')

    useEffect(() => {
        const defaultModel = models.find((model) => model.defaultProfiles.includes(agent?.profile || '')) || models[0]
        if (!defaultModel) {
            setSelectedProviderID('')
            setSelectedModelID('')
            setReasoning('')
            return
        }
        setSelectedProviderID(defaultModel.providerId)
        setSelectedModelID(defaultModel.modelId)
        setReasoning(defaultModel.reasoning.supported ? defaultModel.reasoning.default : '')
    }, [agent?.id, modelCatalogKey])

    const selectSession = useCallback((id: string) => {
        viewRequestRef.current += 1
        setError('')
        setErrorCode(undefined)
        setSelectedSessionID(id)
        setSession(null)
        setAgent(null)
        setAgentLoading(false)
        setHistory([])
        setStreamingOutputs({})
        setAwaitingOutput(false)
        setInput('')
        setSelectedAgentID('')
    }, [])

    const clearError = useCallback(() => {
        setError('')
        setErrorCode(undefined)
    }, [])

    const selectModel = useCallback((providerId: string, modelId: string) => {
        const model = models.find((item) => item.providerId === providerId && item.modelId === modelId)
        if (!model) {
            return
        }
        setSelectedProviderID(model.providerId)
        setSelectedModelID(model.modelId)
        setReasoning(model.reasoning.supported ? model.reasoning.default : '')
    }, [models])

    const selectReasoning = useCallback((level: string) => {
        const model = models.find((item) => item.providerId === selectedProviderID && item.modelId === selectedModelID)
        if (!model?.reasoning.supported || !model.reasoning.levels.includes(level)) {
            return
        }
        setReasoning(level)
    }, [models, selectedProviderID, selectedModelID])

    const selectAgent = useCallback(async (id: string) => {
        if (id === selectedAgentID) {
            return
        }
        const requestID = ++viewRequestRef.current
        setSelectedAgentID(id)
        setAgent(null)
        setAgentLoading(true)
        setHistory([])
        setAwaitingOutput(false)
        setInput('')
        setError('')
        setErrorCode(undefined)
        try {
            const [nextAgent, nextHistory] = await Promise.all([
                getAgent(id),
                listAgentHistory(id),
            ])
            if (requestID !== viewRequestRef.current) {
                return
            }
            setAgent(nextAgent)
            setAgentLoading(false)
            setHistory(nextHistory)
            setStreamingOutputs((current) => clearDurableStreamingOutput(current, id, nextHistory))
        } catch (err) {
            if (requestID !== viewRequestRef.current) {
                return
            }
            setAgentLoading(false)
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
        }
    }, [selectedAgentID])

    const submitInput = useCallback(async () => {
        if (!session || !input.trim() || busy) {
            return
        }
        setBusy(true)
        setAwaitingOutput(true)
        setError('')
        setErrorCode(undefined)
        try {
            await sendInput({
                sessionId: session.id,
                agentId: agent?.id || '',
                requestId: crypto.randomUUID(),
                content: input,
                providerId: selectedProviderID,
                modelId: selectedModelID,
                reasoning,
                ...runtime,
            })
            setInput('')
            await refresh()
        } catch (err) {
            setAwaitingOutput(false)
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
        } finally {
            setBusy(false)
        }
    }, [agent, busy, input, reasoning, refresh, selectedProviderID, selectedModelID])

    const control = useCallback(async (kind: 'pause' | 'close') => {
        if (!agent || busy) {
            return
        }
        setBusy(true)
        setAwaitingOutput(false)
        setError('')
        setErrorCode(undefined)
        try {
            await requestControl({
                id: crypto.randomUUID(),
                agentId: agent.id,
                kind,
            })
            await refresh()
        } catch (err) {
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
        } finally {
            setBusy(false)
        }
    }, [agent, busy, refresh])

    const bridgeAvailable = isBindingAvailable()
    const bridgeState = useMemo(() => {
        if (!bridgeAvailable) {
            return 'Web preview'
        }
        if (health.issue) {
            return 'Configuration error'
        }
        return health.ready ? 'Ready' : 'Recovering'
    }, [bridgeAvailable, health.issue, health.ready])

    return {
        agent,
        agentLoading,
        bridgeAvailable,
        bridgeState,
        busy,
        clearError,
        control,
        error,
        errorCode,
        health,
        history,
        streamingOutput,
        awaitingOutput,
        input,
        models,
        reasoning,
        refresh,
        refreshing,
        selectAgent,
        selectedAgentID,
        selectedSessionID,
        selectSession,
        session,
        sessions,
        projects,
        selectedModelID,
        selectedProviderID,
        selectModel,
        selectReasoning,
        setInput,
        submitInput,
    }
}

function startupIssueMessage(issue: StartupIssue) {
    if (issue.path) {
        return `${issue.path}: ${issue.message}`
    }
    return issue.message
}

function apiErrorCode(error: unknown): ApiErrorCode | undefined {
    if (!(error instanceof Error) || !isApiErrorCode(error.message)) {
        return undefined
    }
    return error.message
}
