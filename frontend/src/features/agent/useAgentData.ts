import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type Dispatch, type RefObject, type SetStateAction } from 'react'
import { getAgent, listAgentHistory, subscribeAgentEvents, type AgentHistoryItem, type AgentSnapshot, type ApiErrorCode, type ModelOption } from '../../api'
import { apiErrorCode, readableError } from '../../shared/errors'
import { createAgentStore } from './agentStore'
import { loadLatestHistory } from './history'
import { useAgentTaskInput } from './useAgentTaskInput'

type AgentDataOptions = {
    models: ModelOption[]
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
    viewRequestRef: RefObject<number>
}
const emptyHistory: AgentHistoryItem[] = []

/** 管理会话查询与选择；实时输出由对话区域直接订阅，不触发工作区重渲染。 */
export function useAgentData({ models, setError, setErrorCode, viewRequestRef }: AgentDataOptions) {
    const [store] = useState(createAgentStore)
    const data = useSyncExternalStore(store.subscribeData, store.getData)
    const selected = data.byId[data.selectedId]
    const agent = selected?.agent || null
    const taskInput = useAgentTaskInput(agent, models)
    const { setInput } = taskInput
    const generation = useRef(0)
    const requests = useRef(new Map<string, number>())

    const loadAgent = useCallback(async (id: string) => {
        const entry = store.getData().byId[id]
        if (!entry) return
        const sessionRequest = generation.current
        const request = (requests.current.get(id) || 0) + 1
        requests.current.set(id, request)
        store.startLoading(id)
        const [agentResult, historyResult] = await Promise.allSettled([getAgent(id), loadLatestHistory(id, entry.history || emptyHistory, listAgentHistory)])
        if (generation.current !== sessionRequest || requests.current.get(id) !== request) return
        const currentAgent = store.getData().byId[id].agent
        // 查询期间新任务已开始时，保留事件确认的新任务身份。
        const nextAgent = agentResult.status === 'fulfilled' && currentAgent.currentExecutionId === entry.agent.currentExecutionId ? agentResult.value : currentAgent
        const error = agentResult.status === 'rejected' ? agentResult.reason : historyResult.status === 'rejected' ? historyResult.reason : undefined
        store.loaded(id, nextAgent, historyResult.status === 'fulfilled' ? historyResult.value : undefined, error ? readableError(error) : '')
        if (error && store.getSelectedAgentID() === id) {
            setError(readableError(error))
            setErrorCode(apiErrorCode(error))
        }
    }, [store, setError, setErrorCode])

    const loadForSession = useCallback(async ({ agents }: { agents: AgentSnapshot[] }, requestID: number) => {
        if (viewRequestRef.current !== requestID) return
        generation.current += 1
        requests.current.clear()
        store.setRoster(agents)
        await Promise.all(agents.map((item) => loadAgent(item.id)))
    }, [store, loadAgent, viewRequestRef])

    const resetForSessionChange = useCallback(() => {
        generation.current += 1
        requests.current.clear()
        store.reset()
        setInput('')
    }, [store, setInput])

    const selectAgent = useCallback(async (id: string) => {
        const current = store.getData()
        if (!current.byId[id]) return
        if (current.selectedId !== id) {
            store.select(id)
            setInput('')
        }
        setError('')
        setErrorCode(undefined)
        await loadAgent(id)
    }, [store, loadAgent, setError, setErrorCode, setInput])

    const loadOlderHistory = useCallback(async () => {
        const id = store.getSelectedAgentID()
        const entry = store.getData().byId[id]
        if (!entry?.nextCursor || entry.loadingOlder) return
        const sessionRequest = generation.current
        const cursor = entry.nextCursor
        store.startOlder(id)
        try {
            const page = await listAgentHistory(id, cursor)
            if (generation.current === sessionRequest) store.olderLoaded(id, page)
        } catch (error) {
            if (generation.current === sessionRequest) store.olderLoaded(id, undefined, readableError(error))
        }
    }, [store])

    useEffect(() => {
        const unsubscribe = subscribeAgentEvents((event) => {
            const previousExecutionID = store.getData().byId[event.agentId]?.agent.currentExecutionId
            store.receive(event)
            // 新执行领取队首后刷新队列；同一次执行的后续 turn 不重复查询。
            const executionStarted = event.kind === 'turn_started' && event.executionId !== previousExecutionID
            if (executionStarted || event.kind === 'execution_ended' || event.kind === 'request_canceled') void loadAgent(event.agentId)
        })
        return () => { generation.current += 1; unsubscribe() }
    }, [store, loadAgent])

    const agents = useMemo(() => data.ids.map((id) => data.byId[id].agent), [data.ids, data.byId])
    const agentHistories = useMemo(() => Object.fromEntries(data.ids.map((id) => [id, data.byId[id].history || emptyHistory])), [data.ids, data.byId])
    const agentHistoryErrors = useMemo(() => Object.fromEntries(data.ids.filter((id) => data.byId[id].error).map((id) => [id, data.byId[id].error])), [data.ids, data.byId])
    const inputActions = useMemo(() => ({ beginInput: store.beginInput, acceptInput: store.acceptInput, rejectInput: store.rejectInput, stopAwaiting: store.stopAwaiting }), [store])
    const outputStore = useMemo(() => ({ subscribeOutput: store.subscribeOutput, getOutput: store.getOutput }), [store])

    return {
        ...taskInput,
        agent, agents, agentHistories, agentHistoryErrors,
        agentLoading: selected?.loading ?? false,
        history: selected?.history || emptyHistory,
        historyCursor: selected?.nextCursor || '',
        historyLoadingOlder: selected?.loadingOlder ?? false,
        historyError: selected?.error || '',
        loadOlderHistory,
        selectedAgentID: data.selectedId,
        getSelectedAgentID: store.getSelectedAgentID,
        outputStore,
        inputActions,
        clearSessionData: resetForSessionChange,
        resetForSessionChange, loadForSession, selectAgent,
        refreshAgent: loadAgent,
    }
}
