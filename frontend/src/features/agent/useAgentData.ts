import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import { listAgentHistory, type HistoryMessage, type ModelOption } from '../../api'
import { readableError } from '../../shared/errors'
import type { SessionStore } from '../session/sessionStore'
import type { RequestActivities } from './history/entries'
import { useAgentTaskInput } from './useAgentTaskInput'

const emptyEntries: HistoryMessage[] = []
const emptyRequests: RequestActivities = new Map()
/** 正文按 Agent 独立分页；执行状态只接纳同一 Session store 的投影。 */
export function useAgentData({ store, models }: { store: SessionStore; models: ModelOption[] }) {
    const requests = useRef(new Map<string, number>())
    const data = useSyncExternalStore(store.subscribeData, store.getData)
    const selected = data.byId[data.selectedId]
    const taskInput = useAgentTaskInput(data.session ? store.sessionId : '', models)
    const loadHistory = useCallback(async (id: string, older = false) => {
        const current = store.getData().byId[id]
        if (!current || current.loading || older && !current.page?.hasMore) return
        const request = (requests.current.get(id) || 0) + 1
        requests.current.set(id, request)
        store.startLoading(id)
        const belongs = () => requests.current.get(id) === request
        try {
            const page = await listAgentHistory(store.sessionId, id, older ? current.page?.nextCursor : null)
            if (belongs()) store.loaded(id, page)
        } catch (reason) { if (belongs()) store.failed(id, readableError(reason)) }
    }, [store])
    const loadEarlier = useCallback(() => loadHistory(store.getSelectedAgentID(), true), [store, loadHistory])
    const retryHistory = useCallback(() => {
        const id = store.getSelectedAgentID()
        return loadHistory(id, store.getData().byId[id]?.page?.hasMore ?? false)
    }, [store, loadHistory])
    const needsHistory = Boolean(selected && !selected.page && !selected.loading && !selected.error)
    const selectedId = data.selectedId
    useEffect(() => {
        if (needsHistory) void loadHistory(selectedId)
    }, [loadHistory, needsHistory, selectedId])
    useEffect(() => {
        const pending = requests.current
        return () => pending.clear()
    }, [])
    const agents = useMemo(() => data.ids.map((id) => data.byId[id].agent), [data.ids, data.byId])
    const agentEntries = useMemo(() => Object.fromEntries(data.ids.filter((id) => data.byId[id].page || data.byId[id].entries.length).map((id) => [id, data.byId[id].entries])), [data.ids, data.byId])
    const agentEntriesErrors = useMemo(() => Object.fromEntries(data.ids.filter((id) => data.byId[id].error).map((id) => [id, data.byId[id].error])), [data.ids, data.byId])
    const outputStore = useMemo(() => ({ subscribeOutput: store.subscribeOutput, getOutput: store.getOutput }), [store])
    return {
        ...taskInput,
        agent: selected?.agent || null, agents, agentEntries, agentEntriesErrors,
        agentLoading: Boolean(selected?.loading && !selected.entries.length),
        historyLoading: selected?.loading ?? false, hasEarlier: selected?.page?.hasMore ?? false,
        entries: selected?.entries ?? emptyEntries, requests: selected?.requests ?? emptyRequests, entriesError: selected?.error || '',
        selectedAgentID: data.selectedId, getSelectedAgentID: store.getSelectedAgentID,
        outputStore, selectAgent: store.select, loadEarlier, retryHistory,
    }
}
