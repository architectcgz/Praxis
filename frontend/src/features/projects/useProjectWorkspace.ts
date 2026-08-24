import {useCallback, useEffect, useMemo, useRef, useState} from 'react'
import {
    AgentHistoryItem,
	AgentOutputEvent,
    AgentSnapshot,
    HealthSnapshot,
	ModelOption,
    SessionSnapshot,
    SessionSummary,
	StartupIssue,
    getAgent,
    getSession,
    isBindingAvailable,
    listAgentHistory,
	listModels,
    listSessions,
    loadReadiness,
    requestControl,
    sendInput,
	subscribeAgentOutput,
} from '../../shared/api'
import {readableError} from './errors'
import {readRememberedSession, rememberSelectedSession} from './sessionPreference'
import {StreamingOutput} from './types'

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
    const remaining = {...outputs}
    delete remaining[agentID]
    return remaining
}

export function useProjectWorkspace() {
    const [health, setHealth] = useState<HealthSnapshot>({ready: false})
    const [sessions, setSessions] = useState<SessionSummary[]>([])
    const [selectedSessionID, setSelectedSessionID] = useState(readRememberedSession)
    const [session, setSession] = useState<SessionSnapshot | null>(null)
    const [selectedAgentID, setSelectedAgentID] = useState('')
	const [agent, setAgent] = useState<AgentSnapshot | null>(null)
	const [agentLoading, setAgentLoading] = useState(false)
	const [history, setHistory] = useState<AgentHistoryItem[]>([])
	const [streamingOutputs, setStreamingOutputs] = useState<StreamingOutputs>({})
	const [awaitingOutput, setAwaitingOutput] = useState(false)
	const [models, setModels] = useState<ModelOption[]>([])
	const [selectedModelID, setSelectedModelID] = useState('')
	const [reasoning, setReasoning] = useState('')
    const [input, setInput] = useState('')
    const [error, setError] = useState('')
    const [busy, setBusy] = useState(false)
    const [refreshing, setRefreshing] = useState(false)
    const viewRequestRef = useRef(0)
	const refreshRef = useRef<(manual?: boolean) => Promise<void>>(async () => {})
	const streamingOutput = agent ? streamingOutputs[agent.id] || null : null

    const refresh = useCallback(async (manual = false) => {
        const requestID = ++viewRequestRef.current
        const isCurrentRequest = () => viewRequestRef.current === requestID
        setError('')
        if (!isBindingAvailable()) {
            setHealth({ready: false})
            setSessions([])
            setSession(null)
            setAgent(null)
			setAgentLoading(false)
            setHistory([])
			setStreamingOutputs({})
			setModels([])
            if (manual) {
                setError('Wails bridge is unavailable. Run the desktop app to refresh workspace data.')
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
				} else if (manual) {
                    setError('Praxis is still starting. Try refreshing again in a moment.')
                }
                return
            }

            const [catalog, configuredModels] = await Promise.all([listSessions(), listModels()])
            if (!isCurrentRequest()) {
                return
            }
            catalogSessions = catalog
            setModels(configuredModels)
            setSessions(catalogSessions)
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
				return {...current, [event.agentId]: {...output, content: output.content + event.text}}
			}
			return {...current, [event.agentId]: {executionId: event.executionId, content: event.text}}
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
        model.id,
        model.reasoning.supported,
        model.reasoning.levels.join(','),
        model.reasoning.default,
        model.defaultProfiles.join(','),
    ].join(':')).join('|')

    useEffect(() => {
        const defaultModel = models.find((model) => model.defaultProfiles.includes(agent?.profile || '')) || models[0]
        if (!defaultModel) {
            setSelectedModelID('')
            setReasoning('')
            return
        }
        setSelectedModelID(defaultModel.id)
        setReasoning(defaultModel.reasoning.supported ? defaultModel.reasoning.default : '')
    }, [agent?.id, modelCatalogKey])

    const selectSession = useCallback((id: string) => {
        viewRequestRef.current += 1
        setError('')
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
    }, [])

    const selectModel = useCallback((id: string) => {
        const model = models.find((item) => item.id === id)
        if (!model) {
            return
        }
        setSelectedModelID(model.id)
        setReasoning(model.reasoning.supported ? model.reasoning.default : '')
    }, [models])

    const selectReasoning = useCallback((level: string) => {
        const model = models.find((item) => item.id === selectedModelID)
        if (!model?.reasoning.supported || !model.reasoning.levels.includes(level)) {
            return
        }
        setReasoning(level)
    }, [models, selectedModelID])

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
		}
	}, [selectedAgentID])

    const submitInput = useCallback(async () => {
        if (!agent || !input.trim() || busy) {
            return
		}
		setBusy(true)
		setAwaitingOutput(true)
		setError('')
        try {
            await sendInput({
                agentId: agent.id,
                requestId: crypto.randomUUID(),
                content: input,
			modelId: selectedModelID,
			reasoning,
                ...runtime,
            })
            setInput('')
            await refresh()
		} catch (err) {
			setAwaitingOutput(false)
			setError(readableError(err))
        } finally {
            setBusy(false)
        }
    }, [agent, busy, input, reasoning, refresh, selectedModelID])

    const control = useCallback(async (kind: 'pause' | 'close') => {
        if (!agent || busy) {
            return
		}
		setBusy(true)
		setAwaitingOutput(false)
		setError('')
        try {
            await requestControl({
                id: crypto.randomUUID(),
                agentId: agent.id,
                kind,
            })
            await refresh()
        } catch (err) {
            setError(readableError(err))
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
		selectedModelID,
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
