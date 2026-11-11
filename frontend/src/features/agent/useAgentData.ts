import { useCallback, useEffect, useMemo, useRef, useState, useSyncExternalStore, type Dispatch, type SetStateAction } from 'react'
import { listAgentEntries, subscribeAgentEvents, type Entry, type ApiErrorCode, type ModelOption, type SessionSnapshot } from '../../api'
import { readableError } from '../../shared/errors'
import { createAgentStore } from './agentStore'
import { useAgentTaskInput } from './useAgentTaskInput'

type AgentDataOptions = {
    sessionID: string
    models: ModelOption[]
    setError: Dispatch<SetStateAction<string>>
    setErrorCode: Dispatch<SetStateAction<ApiErrorCode | undefined>>
}
const emptyEntries: Entry[] = []

/** 唯一会话页持有当前分支正文；已结束子代理仅在被选择时请求。 */
export function useAgentData({ sessionID, models, setError, setErrorCode }: AgentDataOptions) {
    const [store] = useState(createAgentStore)
    const requests = useRef(new Map<string, number>())
    const data = useSyncExternalStore(store.subscribeData, store.getData)
    const selected = data.byId[data.selectedId]
    const taskInput = useAgentTaskInput(sessionID, models, setError, setErrorCode)
    const { setInput } = taskInput

    const loadForSession = useCallback(async ({ agents, id, branch, entries }: SessionSnapshot) => {
        const scope = store.getScope()
        if (scope.sessionId !== id || scope.branchId !== branch.id) {
            requests.current.clear()
            store.reset()
        }
        store.setScope(id, branch.id)
        store.setRoster(agents)
        for (const agent of agents) {
            let page = entries[agent.ID]
            if (!page) continue
            const current = store.getData().byId[agent.ID]
            const latest = current?.entries?.at(-1)?.id
            while (latest && page.hasMore && !page.entries.some((entry) => entry.id === latest)) {
                const earlier = await listAgentEntries(id, branch.id, agent.ID, page.headEntryId, page.nextCursor)
                if (store.getScope().branchId !== branch.id) return
                page = { ...page, entries: [...earlier.entries, ...page.entries], hasMore: earlier.hasMore, nextCursor: earlier.nextCursor }
            }
            if (store.getScope().branchId === branch.id) store.loaded(agent.ID, agent, page)
        }
    }, [store])

    const loadHistory = useCallback(async (id: string, older = false) => {
        const scope = store.getScope()
        const current = store.getData().byId[id]
        if (!current || current.loading || older && !current.page?.hasMore) return
        const request = (requests.current.get(id) || 0) + 1
        requests.current.set(id, request)
        store.startLoading(id)
        const belongs = () => requests.current.get(id) === request && store.getScope() === scope &&
            (store.getSelectedAgentID() === id || Boolean(store.getData().byId[id]?.agent.currentRunId))
        try {
            const page = await listAgentEntries(scope.sessionId, scope.branchId, id, older ? current.page?.headEntryId : '', older ? current.page?.nextCursor : '')
            if (belongs()) store.loaded(id, store.getData().byId[id].agent, page, older)
        } catch (reason) {
            if (belongs()) store.failed(id, readableError(reason))
        }
    }, [store])

    const selectAgent = useCallback((id: string) => {
        if (!store.getData().byId[id]) return
        if (store.getSelectedAgentID() !== id) {
            const previous = store.getSelectedAgentID()
            requests.current.set(previous, (requests.current.get(previous) || 0) + 1)
            store.select(id)
            setInput('')
        }
        if (store.getData().byId[id].entries === undefined) void loadHistory(id)
        setError('')
        setErrorCode(undefined)
    }, [store, loadHistory, setInput, setError, setErrorCode])

    const loadEarlier = useCallback(() => loadHistory(store.getSelectedAgentID(), true), [store, loadHistory])
    const retryHistory = useCallback(() => loadHistory(store.getSelectedAgentID()), [store, loadHistory])
    useEffect(() => subscribeAgentEvents(store.receive), [store])
    const agents = useMemo(() => data.ids.map((id) => data.byId[id].agent), [data.ids, data.byId])
    const agentEntries = useMemo(() => Object.fromEntries(data.ids.filter((id) => data.byId[id].entries !== undefined).map((id) => [id, data.byId[id].entries!])), [data.ids, data.byId])
    const agentEntriesErrors = useMemo(() => Object.fromEntries(data.ids.filter((id) => data.byId[id].error).map((id) => [id, data.byId[id].error])), [data.ids, data.byId])
    const inputActions = useMemo(() => ({ beginInput: store.beginInput, acceptInput: store.acceptInput, rejectInput: store.rejectInput, stopAwaiting: store.stopAwaiting }), [store])
    const outputStore = useMemo(() => ({ subscribeOutput: store.subscribeOutput, getOutput: store.getOutput }), [store])
    return {
        ...taskInput,
        agent: selected?.agent || null, agents, agentEntries, agentEntriesErrors,
        agentLoading: Boolean(selected?.loading && selected.entries === undefined),
        historyLoading: selected?.loading ?? false, hasEarlier: selected?.page?.hasMore ?? false,
        entries: selected?.entries || emptyEntries, entriesError: selected?.error || '',
        selectedAgentID: data.selectedId, getSelectedAgentID: store.getSelectedAgentID,
        outputStore, inputActions, loadForSession, selectAgent, loadEarlier, retryHistory,
    }
}
