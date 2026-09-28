import { useCallback, useEffect, useRef, useState } from 'react'
import {
    API_ERROR_CODES,
    AgentHistoryItem,
    AgentEvent,
    AgentSnapshot,
    ModelOption,
    ProjectSummary,
    SessionSnapshot,
    SessionSummary,
    getAgent,
    getSession,
    isBindingAvailable,
    listAgentHistory,
    listModels,
    listProjects,
    listSessions,
    closeAgent,
    pauseAgent,
    createSession,
    sendInput,
    subscribeAgentEvents,
    isApiErrorCode,
    parseApiError,
    type ApiErrorCode,
} from '../../api'
import { readableError } from '../../shared/errors'
import { readRememberedSession, rememberSelectedSession } from './sessionPreference'
import { PendingUserMessage, StreamingOutput } from '../sessions/types'

type StreamingOutputs = Record<string, StreamingOutput>

function clearDurableStreamingOutput(outputs: StreamingOutputs, agent: AgentSnapshot, history: AgentHistoryItem[]): StreamingOutputs {
    const agentID = agent.id
    const output = outputs[agentID]
    if (!output) {
        return outputs
    }
    const settled = agent.executions.some((execution) => execution.id === output.executionId && execution.status === 'settled')
    const persisted = history.some((item) => (
        item.message?.role === 'assistant' && item.message.executionId === output.executionId
    ) || item.execution?.id === output.executionId)
    const hasToolOutput = output.turns.some((turn) => turn.events.some((event) => event.kind === 'tool_call' || event.kind === 'tool_result'))
    const persistedToolOutput = history.some((item) => (
        item.message?.executionId === output.executionId &&
        item.message.blocks?.some((block) => block.kind === 'tool_call' || block.kind === 'tool_result')
    ))
    if (!settled || !persisted || hasToolOutput && !persistedToolOutput) {
        return outputs
    }
    const remaining = { ...outputs }
    delete remaining[agentID]
    return remaining
}

// Drop optimistic messages once the durable transcript contains their execution's user message.
function clearDurablePendingUserMessages(pending: PendingUserMessage[], history: AgentHistoryItem[]): PendingUserMessage[] {
    if (pending.length === 0) {
        return pending
    }
    const persistedExecutionIDs = new Set<string>()
    for (const item of history) {
        if (item.message?.role === 'user' && item.message.executionId) {
            persistedExecutionIDs.add(item.message.executionId)
        }
    }
    if (persistedExecutionIDs.size === 0) {
        return pending
    }
    const remaining = pending.filter((message) => message.executionId === '' || !persistedExecutionIDs.has(message.executionId))
    return remaining.length === pending.length ? pending : remaining
}

export function useProjectWorkspace() {
    const [sessions, setSessions] = useState<SessionSummary[]>([])
    const [projects, setProjects] = useState<ProjectSummary[]>([])
    const [selectedSessionID, setSelectedSessionID] = useState(readRememberedSession)
    const [session, setSession] = useState<SessionSnapshot | null>(null)
    const [selectedAgentID, setSelectedAgentID] = useState('')
    const [agent, setAgent] = useState<AgentSnapshot | null>(null)
    const [agentLoading, setAgentLoading] = useState(false)
    const [history, setHistory] = useState<AgentHistoryItem[]>([])
    const [streamingOutputs, setStreamingOutputs] = useState<StreamingOutputs>({})
    const [pendingUserMessages, setPendingUserMessages] = useState<PendingUserMessage[]>([])
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
            const primaryConfigured = configuredModels.some((model) => model.assignedAgentDefinitions.includes('primary'))
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
            setStreamingOutputs((current) => clearDurableStreamingOutput(current, nextAgent, nextHistory))
            setPendingUserMessages((current) => clearDurablePendingUserMessages(current, nextHistory))
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
    }, [refresh])

    useEffect(() => subscribeAgentEvents((event: AgentEvent) => {
        if (event.kind === 'settled') {
            void refreshRef.current()
            return
        }
        if (event.kind === 'turn_started' || event.kind === 'turn_completed') {
            return
        }
        setStreamingOutputs((current) => {
            const previous = current[event.agentId]
            const output: StreamingOutput = previous?.executionId === event.executionId
                ? previous
                : { executionId: event.executionId, turns: [], error: '' }
            switch (event.kind) {
                case 'text_delta':
                case 'thinking_delta':
                case 'tool_call':
                case 'tool_result': {
                    const turn = event.turn || 1
                    const turns = [...output.turns]
                    const index = turns.findIndex((item) => item.turn === turn)
                    const previousTurn = turns[index] || { turn, events: [] }
                    const events = [...previousTurn.events]
                    const last = events[events.length - 1]
                    if ((event.kind === 'text_delta' || event.kind === 'thinking_delta') && last?.kind === event.kind) {
                        events[events.length - 1] = { ...last, text: (last.text || '') + (event.text || '') }
                    } else {
                        events.push(event)
                    }
                    const nextTurn = { ...previousTurn, events }
                    if (index < 0) {
                        turns.push(nextTurn)
                    } else {
                        turns[index] = nextTurn
                    }
                    return { ...current, [event.agentId]: { ...output, turns } }
                }
                case 'error':
                    return { ...current, [event.agentId]: { ...output, error: event.error || '' } }
            }
            return current
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
        model.reasoningLevels.join(','),
        model.defaultReasoningLevel,
        model.defaultProviderId,
        model.defaultModelId,
        model.assignedAgentDefinitions.join(','),
    ].join(':')).join('|')

    useEffect(() => {
        const defaultModel = models.find((model) => model.assignedAgentDefinitions.includes(agent?.definitionId || '')) ||
            models.find((model) => model.providerId === model.defaultProviderId && model.modelId === model.defaultModelId) ||
            models.find((model) => model.providerId === model.defaultProviderId) ||
            models[0]
        if (!defaultModel) {
            setSelectedProviderID('')
            setSelectedModelID('')
            setReasoning('')
            return
        }
        setSelectedProviderID(defaultModel.providerId)
        setSelectedModelID(defaultModel.modelId)
        setReasoning(defaultModel.reasoningLevels.length > 0 ? defaultModel.defaultReasoningLevel : '')
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

    const createNewSession = useCallback(async (projectID: string, workspaceID: string) => {
        if (busy || !projectID || !workspaceID || !isBindingAvailable()) {
            return
        }
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
            selectSession(created.sessionId)
        } catch (err) {
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
        } finally {
            setBusy(false)
        }
    }, [busy, selectSession])

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
        setReasoning(model.reasoningLevels.length > 0 ? model.defaultReasoningLevel : '')
    }, [models])

    const selectReasoning = useCallback((level: string) => {
        const model = models.find((item) => item.providerId === selectedProviderID && item.modelId === selectedModelID)
        if (!model?.reasoningLevels.includes(level)) {
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
            setStreamingOutputs((current) => clearDurableStreamingOutput(current, nextAgent, nextHistory))
            setPendingUserMessages((current) => clearDurablePendingUserMessages(current, nextHistory))
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
        if (!selectedProviderID.trim() || !selectedModelID.trim()) {
            setError('Select a configured Provider and Model before sending.')
            setErrorCode(API_ERROR_CODES.modelNotConfigured)
            return
        }
        const requestID = crypto.randomUUID()
        const content = input
        const agentID = agent?.id || ''
        setBusy(true)
        setAwaitingOutput(true)
        setPendingUserMessages((current) => [
            ...current,
            { requestId: requestID, executionId: '', sessionId: session.id, agentId: agentID, content, at: new Date().toISOString() },
        ])
        setError('')
        setErrorCode(undefined)
        try {
            const response = await sendInput({
                sessionId: session.id,
                agentId: agentID,
                requestId: requestID,
                content,
                providerId: selectedProviderID,
                modelId: selectedModelID,
                reasoningLevel: reasoning,
            })
            setPendingUserMessages((current) => current.map((message) => (
                message.requestId === requestID ? { ...message, executionId: response.executionId } : message
            )))
            setInput('')
            await refresh()
        } catch (err) {
            setPendingUserMessages((current) => current.filter((message) => message.requestId !== requestID))
            setAwaitingOutput(false)
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
        } finally {
            setBusy(false)
        }
    }, [agent, busy, input, reasoning, refresh, session, selectedProviderID, selectedModelID])

    const control = useCallback(async (kind: 'pause' | 'close') => {
        if (!agent || busy) {
            return
        }
        setBusy(true)
        setAwaitingOutput(false)
        setError('')
        setErrorCode(undefined)
        try {
            const commandId = crypto.randomUUID()
            if (kind === 'pause') {
                await pauseAgent({ commandId, agentId: agent.id })
            } else {
                await closeAgent({ commandId, agentId: agent.id })
            }
            await refresh()
        } catch (err) {
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
        } finally {
            setBusy(false)
        }
    }, [agent, busy, refresh])

    const bridgeAvailable = isBindingAvailable()

    return {
        agent,
        agentLoading,
        bridgeAvailable,
        busy,
        clearError,
        control,
        createNewSession,
        error,
        errorCode,
        history,
        pendingUserMessages,
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

function apiErrorCode(error: unknown): ApiErrorCode | undefined {
    const parsed = parseApiError(error)
    if (!parsed || !isApiErrorCode(parsed.code)) {
        return undefined
    }
    return parsed.code
}
