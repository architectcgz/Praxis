import { useCallback, useEffect, useRef, useState, type Dispatch, type RefObject, type SetStateAction } from 'react'
import {
    getAgent,
    listAgentHistory,
    subscribeAgentEvents,
    type AgentEvent,
    type AgentHistoryItem,
    type AgentSnapshot,
    type ApiErrorCode,
    type ModelOption,
} from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import type { PendingUserMessage, StreamingOutputs } from './types'
import { clearDurablePendingUserMessages, clearDurableStreamingOutput, mergeStreamingEvent } from './streaming'
import { useAgentTaskInput } from './useAgentTaskInput'

type AgentSessionContext = { agents: AgentSnapshot[] }

type AgentDataOptions = {
    models: ModelOption[]
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    viewRequestRef: RefObject<number>
}

/** 管理当前会话中的 Agent、历史记录和实时输出。 */
export function useAgentData({ models, setError, setErrorCode, viewRequestRef }: AgentDataOptions) {
    const [selectedAgentID, setSelectedAgentID] = useState('')
    const selectedAgentIDRef = useRef(selectedAgentID)
    selectedAgentIDRef.current = selectedAgentID
    const [agent, setAgent] = useState<AgentSnapshot | null>(null)
    const agentRef = useRef(agent)
    agentRef.current = agent
    const [agentLoading, setAgentLoading] = useState(false)
    const [agents, setAgents] = useState<AgentSnapshot[]>([])
    const [history, setHistory] = useState<AgentHistoryItem[]>([])
    const [agentHistories, setAgentHistories] = useState<Record<string, AgentHistoryItem[]>>({})
    const agentHistoriesRef = useRef(agentHistories)
    agentHistoriesRef.current = agentHistories
    const [agentHistoryErrors, setAgentHistoryErrors] = useState<Record<string, string>>({})
    const [streamingOutputs, setStreamingOutputs] = useState<StreamingOutputs>({})
    const [pendingUserMessages, setPendingUserMessages] = useState<PendingUserMessage[]>([])
    const [awaitingOutput, setAwaitingOutput] = useState(false)
    const taskInput = useAgentTaskInput(agent, models)
    const { setInput } = taskInput
    const sessionAgentIDsRef = useRef(new Set<string>())
    const sessionAgentsRef = useRef(new Map<string, AgentSnapshot>())
    const sessionDataRequestRef = useRef(0)
    const agentSelectionRequestRef = useRef(0)
    const agentDataRequestRef = useRef(new Map<string, number>())
    const streamingOutput = agent ? streamingOutputs[agent.id] || null : null

    const loadForSession = useCallback(async ({ agents: sessionAgents }: AgentSessionContext, requestID: number) => {
        const sessionRequestID = ++sessionDataRequestRef.current
        const selectionRequestID = ++agentSelectionRequestRef.current
        agentDataRequestRef.current.clear()
        sessionAgentIDsRef.current = new Set(sessionAgents.map((item) => item.id))
        sessionAgentsRef.current = new Map(sessionAgents.map((item) => [item.id, item]))
        setAgents(sessionAgents)
        const selectedID = selectedAgentIDRef.current
        const nextAgentID = sessionAgents.some((item) => item.id === selectedID)
            ? selectedID
            : sessionAgents[0]?.id || ''
        if (!nextAgentID) {
            selectedAgentIDRef.current = ''
            setSelectedAgentID('')
            setAgent(null)
            setAgentLoading(false)
            setHistory([])
            setStreamingOutputs({})
            return
        }

        selectedAgentIDRef.current = nextAgentID
        setSelectedAgentID(nextAgentID)
        const selectedAgentDataRequestID = (agentDataRequestRef.current.get(nextAgentID) || 0) + 1
        agentDataRequestRef.current.set(nextAgentID, selectedAgentDataRequestID)
        if (agentRef.current?.id !== nextAgentID) {
            setAgentLoading(true)
        }

        const [agentResult, historyResult] = await Promise.allSettled([
            getAgent(nextAgentID),
            listAgentHistory(nextAgentID),
        ])
        if (viewRequestRef.current !== requestID || sessionDataRequestRef.current !== sessionRequestID ||
            agentDataRequestRef.current.get(nextAgentID) !== selectedAgentDataRequestID) {
            return
        }
        const nextAgent = agentResult.status === 'fulfilled'
            ? agentResult.value
            : sessionAgentsRef.current.get(nextAgentID)!
        const nextHistory = historyResult.status === 'fulfilled'
            ? historyResult.value
            : agentHistoriesRef.current[nextAgentID] || []
        const loadError = agentResult.status === 'rejected'
            ? agentResult.reason
            : historyResult.status === 'rejected' ? historyResult.reason : undefined

        setAgentHistories((current) => ({ ...current, [nextAgentID]: nextHistory }))
        agentHistoriesRef.current = { ...agentHistoriesRef.current, [nextAgentID]: nextHistory }
        sessionAgentsRef.current.set(nextAgentID, nextAgent)
        setAgents((current) => current.map((item) => item.id === nextAgentID ? nextAgent : item))
        setAgentHistoryErrors((current) => {
            const next = { ...current }
            if (historyResult.status === 'rejected') next[nextAgentID] = readableError(historyResult.reason)
            else delete next[nextAgentID]
            return next
        })
        setStreamingOutputs((current) => clearDurableStreamingOutput(current, nextAgent, nextHistory))
        setPendingUserMessages((current) => clearDurablePendingUserMessages(current, nextHistory))
        if (agentSelectionRequestRef.current === selectionRequestID) {
            setAgent(nextAgent)
            setAgentLoading(false)
            setHistory(nextHistory)
            if (loadError) {
                setError(readableError(loadError))
                setErrorCode(apiErrorCode(loadError))
            }
        }

        const otherAgents = sessionAgents.filter((item) => item.id !== nextAgentID)
        const historyRequestIDs = otherAgents.map((item) => {
            const requestID = (agentDataRequestRef.current.get(item.id) || 0) + 1
            agentDataRequestRef.current.set(item.id, requestID)
            return requestID
        })
        void Promise.allSettled(otherAgents.map((item) => listAgentHistory(item.id))).then((results) => {
            if (viewRequestRef.current !== requestID || sessionDataRequestRef.current !== sessionRequestID) {
                return
            }
            const nextHistories: Record<string, AgentHistoryItem[]> = {}
            const nextHistoryErrors: Record<string, string> = {}
            results.forEach((result, index) => {
                const id = otherAgents[index].id
                if (agentDataRequestRef.current.get(id) !== historyRequestIDs[index]) return
                if (result.status === 'fulfilled') nextHistories[id] = result.value
                else nextHistoryErrors[id] = readableError(result.reason)
            })
            agentHistoriesRef.current = { ...agentHistoriesRef.current, ...nextHistories }
            setAgentHistories((current) => ({ ...current, ...nextHistories }))
            setAgentHistoryErrors((current) => {
                const next = { ...current }
                for (const item of otherAgents) delete next[item.id]
                return { ...next, ...nextHistoryErrors }
            })
            setStreamingOutputs((current) => sessionAgents.reduce((outputs, item) => (
                item.id === nextAgentID || !nextHistories[item.id]
                    ? outputs
                    : clearDurableStreamingOutput(outputs, item, nextHistories[item.id])
            ), current))
            setPendingUserMessages((current) => Object.values(nextHistories).reduce(
                clearDurablePendingUserMessages,
                clearDurablePendingUserMessages(current, nextHistory),
            ))
        })
    }, [setError, setErrorCode, viewRequestRef])

    const resetForSessionChange = useCallback(() => {
        sessionDataRequestRef.current += 1
        agentSelectionRequestRef.current += 1
        sessionAgentIDsRef.current.clear()
        sessionAgentsRef.current.clear()
        selectedAgentIDRef.current = ''
        setAgent(null)
        setAgentLoading(false)
        setHistory([])
        setAgentHistories({})
        agentHistoriesRef.current = {}
        setAgentHistoryErrors({})
        setStreamingOutputs({})
        setAwaitingOutput(false)
        setInput('')
        setSelectedAgentID('')
    }, [setInput])

    // 连接不可用、空会话和加载失败共享清理语义；乐观消息仍按原始 Agent / Session 保留。
    const clearSessionData = useCallback(() => {
        resetForSessionChange()
        agentDataRequestRef.current.clear()
        setAgents([])
    }, [resetForSessionChange])

    const refreshAgent = useCallback(async (id: string) => {
        if (!sessionAgentIDsRef.current.has(id)) {
            return
        }
        const sessionRequestID = sessionDataRequestRef.current
        const requestID = (agentDataRequestRef.current.get(id) || 0) + 1
        agentDataRequestRef.current.set(id, requestID)
        try {
            const [nextAgent, nextHistory] = await Promise.all([getAgent(id), listAgentHistory(id)])
            if (sessionRequestID !== sessionDataRequestRef.current || agentDataRequestRef.current.get(id) !== requestID) {
                return
            }
            sessionAgentsRef.current.set(id, nextAgent)
            agentHistoriesRef.current = { ...agentHistoriesRef.current, [id]: nextHistory }
            setAgents((current) => current.map((item) => item.id === id ? nextAgent : item))
            setAgentHistories((current) => ({ ...current, [id]: nextHistory }))
            setAgentHistoryErrors((current) => {
                const next = { ...current }
                delete next[id]
                return next
            })
            setStreamingOutputs((current) => clearDurableStreamingOutput(current, nextAgent, nextHistory))
            setPendingUserMessages((current) => clearDurablePendingUserMessages(current, nextHistory))
            if (selectedAgentIDRef.current === id) {
                setAgent(nextAgent)
                setAgentLoading(false)
                setHistory(nextHistory)
            }
        } catch (err) {
            if (sessionRequestID !== sessionDataRequestRef.current || agentDataRequestRef.current.get(id) !== requestID) {
                return
            }
            setAgentHistoryErrors((current) => ({ ...current, [id]: readableError(err) }))
            if (selectedAgentIDRef.current === id) {
                setAgentLoading(false)
                setError(readableError(err))
                setErrorCode(apiErrorCode(err))
            }
        }
    }, [setError, setErrorCode])

    const selectAgent = useCallback(async (id: string) => {
        if (id === selectedAgentIDRef.current || !sessionAgentIDsRef.current.has(id)) {
            return
        }
        const requestID = ++agentSelectionRequestRef.current
        const sessionRequestID = sessionDataRequestRef.current
        const agentDataRequestID = (agentDataRequestRef.current.get(id) || 0) + 1
        agentDataRequestRef.current.set(id, agentDataRequestID)
        selectedAgentIDRef.current = id
        setSelectedAgentID(id)
        setAgent(sessionAgentsRef.current.get(id) || null)
        const cachedHistory = agentHistoriesRef.current[id]
        setAgentLoading(cachedHistory === undefined)
        setHistory(cachedHistory || [])
        setAwaitingOutput(false)
        setInput('')
        setError('')
        setErrorCode(undefined)
        try {
            const [nextAgent, nextHistory] = await Promise.all([
                getAgent(id),
                listAgentHistory(id),
            ])
            if (requestID !== agentSelectionRequestRef.current || sessionRequestID !== sessionDataRequestRef.current || agentDataRequestRef.current.get(id) !== agentDataRequestID) {
                return
            }
            agentHistoriesRef.current = { ...agentHistoriesRef.current, [id]: nextHistory }
            sessionAgentsRef.current.set(id, nextAgent)
            setAgents((current) => current.map((item) => item.id === id ? nextAgent : item))
            setAgent(nextAgent)
            setAgentLoading(false)
            setHistory(nextHistory)
            setAgentHistories((current) => ({ ...current, [id]: nextHistory }))
            setStreamingOutputs((current) => clearDurableStreamingOutput(current, nextAgent, nextHistory))
            setPendingUserMessages((current) => clearDurablePendingUserMessages(current, nextHistory))
        } catch (err) {
            if (requestID !== agentSelectionRequestRef.current || sessionRequestID !== sessionDataRequestRef.current || agentDataRequestRef.current.get(id) !== agentDataRequestID) {
                return
            }
            setAgentLoading(false)
            setError(readableError(err))
            setErrorCode(apiErrorCode(err))
        }
    }, [setError, setErrorCode, setInput])

    useEffect(() => subscribeAgentEvents((event: AgentEvent) => {
        if (!sessionAgentIDsRef.current.has(event.agentId)) {
            return
        }
        if (event.kind === 'turn_ended' || event.kind === 'request_canceled') {
            setStreamingOutputs((current) => mergeStreamingEvent(current, event))
            void refreshAgent(event.agentId)
            return
        }
        if (event.kind === 'step_started' || event.kind === 'operation_timing') {
            return
        }
        setStreamingOutputs((current) => mergeStreamingEvent(current, event))
    }), [refreshAgent])

    useEffect(() => {
        if (streamingOutput || agent?.state !== 'executing') {
            setAwaitingOutput(false)
        }
    }, [agent?.state, streamingOutput])

    return {
        ...taskInput,
        agent,
        agentHistories,
        agentHistoryErrors,
        agentLoading,
        agents,
        awaitingOutput,
        clearSessionData,
        history,
        loadForSession,
        pendingUserMessages,
        refreshAgent,
        resetForSessionChange,
        selectAgent,
        selectedAgentID,
        selectedAgentIDRef,
        setAwaitingOutput,
        setPendingUserMessages,
        streamingOutput,
        streamingOutputs,
    }
}
