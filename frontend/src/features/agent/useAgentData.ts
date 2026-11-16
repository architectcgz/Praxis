import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import { listAgentEntries, type Entry, type ModelOption } from '../../api'
import { readableError } from '../../shared/errors'
import type { SessionStore } from '../session/sessionStore'
import { useAgentTaskInput } from './useAgentTaskInput'

const emptyEntries: Entry[] = []
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
            let page = await listAgentEntries(store.sessionId, id, older ? current.page?.headEntryId : '', older ? current.page?.nextCursor : '')
            // 刷新保留已加载历史并补齐与实时记录之间的跨度。
            const latest = !older ? current.entries?.at(-1)?.id : undefined
            while (latest && page.hasMore && !page.entries.some((entry) => entry.id === latest)) {
                const earlier = await listAgentEntries(store.sessionId, id, page.headEntryId, page.nextCursor)
                if (!belongs()) return
                page = { ...page, entries: [...earlier.entries, ...page.entries], hasMore: earlier.hasMore, nextCursor: earlier.nextCursor }
            }
            if (belongs()) store.loaded(id, page, older)
        } catch (reason) { if (belongs()) store.failed(id, readableError(reason)) }
    }, [store])
    const selectAgent = useCallback((id: string) => {
        if (!store.getData().byId[id]) return
        store.select(id)
        if (store.getData().byId[id].entries === undefined) void loadHistory(id)
    }, [store, loadHistory])
    const loadEarlier = useCallback(() => loadHistory(store.getSelectedAgentID(), true), [store, loadHistory])
    const retryHistory = useCallback(() => loadHistory(store.getSelectedAgentID()), [store, loadHistory])
    useEffect(() => {
        const pending = requests.current
        return () => pending.clear()
    }, [])
    const agents = useMemo(() => data.ids.map((id) => data.byId[id].agent), [data.ids, data.byId])
    const agentEntries = useMemo(() => Object.fromEntries(data.ids.filter((id) => data.byId[id].entries !== undefined).map((id) => [id, data.byId[id].entries!])), [data.ids, data.byId])
    const agentEntriesErrors = useMemo(() => Object.fromEntries(data.ids.filter((id) => data.byId[id].error).map((id) => [id, data.byId[id].error])), [data.ids, data.byId])
    const outputStore = useMemo(() => ({ subscribeOutput: store.subscribeOutput, getOutput: store.getOutput }), [store])
    return {
        ...taskInput,
        agent: selected?.agent || null, agents, agentEntries, agentEntriesErrors,
        agentLoading: Boolean(selected?.loading && selected.entries === undefined),
        historyLoading: selected?.loading ?? false, hasEarlier: selected?.page?.hasMore ?? false,
        entries: selected?.entries || emptyEntries, entriesError: selected?.error || '',
        selectedAgentID: data.selectedId, getSelectedAgentID: store.getSelectedAgentID,
        outputStore, selectAgent, loadEarlier, retryHistory,
    }
}
