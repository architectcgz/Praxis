import { useCallback, useEffect, useMemo, useRef, useSyncExternalStore } from 'react'
import { listAgentHistory, type HistoryMessage, type ModelOption, type ToolApproval } from '../../api'
import { readableError } from '../../shared/errors'
import { entryUsage, type SessionStore } from '../session/sessionStore'
import type { RequestActivities } from './history/entries'
import { mergeUsageTotals, type UsageTotals } from './modelUsage'
import { useAgentTaskInput } from './useAgentTaskInput'

const emptyEntries: HistoryMessage[] = []
const emptyRequests: RequestActivities = new Map()
const emptyModels: ModelOption[] = []
const emptyApprovals: ToolApproval[] = []

/** 主代理与被检视子代理各自独立分页，用量按 Agent 独立汇总并合成会话总计。 */
export function useAgentData({ store }: { store: SessionStore }) {
    const requests = useRef(new Map<string, number>())
    const data = useSyncExternalStore(store.subscribeData, store.getData)
    const mainEntry = data.byId[store.sessionId]
    const subagentEntry = data.selectedId !== store.sessionId ? data.byId[data.selectedId] : undefined
    const models = data.session?.models ?? emptyModels
    const approvalMode = data.session?.approvalMode ?? null
    const pendingApprovals = data.session?.pendingApprovals ?? emptyApprovals
    const taskInput = useAgentTaskInput(
        data.session ? store.sessionId : '',
        models,
        data.session?.modelSelection ?? null,
        store.updateModelSelection,
    )

    const loadHistory = useCallback(async (id: string, older = false) => {
        const current = store.getData().byId[id]
        if (!current || current.loading || (older && !current.page?.hasMore)) return
        const request = (requests.current.get(id) || 0) + 1
        requests.current.set(id, request)
        store.startLoading(id)
        const belongs = () => requests.current.get(id) === request
        try {
            const page = await listAgentHistory(store.sessionId, id, older ? current.page?.nextCursor : null)
            if (belongs()) store.loaded(id, page)
        } catch (reason) {
            if (belongs()) store.failed(id, readableError(reason))
        }
    }, [store])

    const loadMainEarlier = useCallback(() => loadHistory(store.sessionId, true), [store, loadHistory])
    const retryMainHistory = useCallback(() => {
        const current = store.getData().byId[store.sessionId]
        return loadHistory(store.sessionId, current?.page?.hasMore ?? false)
    }, [store, loadHistory])

    const loadSubagentEarlier = useCallback(() => {
        const id = store.getSelectedAgentID()
        if (id === store.sessionId) return Promise.resolve()
        return loadHistory(id, true)
    }, [store, loadHistory])

    const retrySubagentHistory = useCallback(() => {
        const id = store.getSelectedAgentID()
        if (id === store.sessionId) return Promise.resolve()
        return loadHistory(id, store.getData().byId[id]?.page?.hasMore ?? false)
    }, [store, loadHistory])

    const selected = data.byId[data.selectedId]
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
    const agentEntries = useMemo(
        () => Object.fromEntries(data.ids.filter((id) => data.byId[id].page || data.byId[id].entries.length).map((id) => [id, data.byId[id].entries])),
        [data.ids, data.byId],
    )
    const agentEntriesErrors = useMemo(
        () => Object.fromEntries(data.ids.filter((id) => data.byId[id].error).map((id) => [id, data.byId[id].error])),
        [data.ids, data.byId],
    )
    const agentUsages = useMemo<Record<string, UsageTotals>>(
        () => Object.fromEntries(data.ids.map((id) => [id, entryUsage(data.byId[id])])),
        [data.ids, data.byId],
    )
    const sessionUsage = useMemo(
        () => mergeUsageTotals(Object.values(agentUsages)),
        [agentUsages],
    )
    const outputStore = useMemo(() => ({ subscribeOutput: store.subscribeOutput, getOutput: store.getOutput }), [store])

    return {
        ...taskInput,
        models,
        approvalMode,
        updateApprovalMode: store.updateApprovalMode,
        pendingApprovals,
        agents,
        agentEntries,
        agentEntriesErrors,
        agentUsages,
        sessionUsage,
        mainAgent: mainEntry?.agent || null,
        mainLoading: Boolean(mainEntry?.loading && !mainEntry.entries.length),
        mainHistoryLoading: mainEntry?.loading ?? false,
        mainHasEarlier: mainEntry?.page?.hasMore ?? false,
        mainEntries: mainEntry?.entries ?? emptyEntries,
        mainRequests: mainEntry?.requests ?? emptyRequests,
        mainEntriesError: mainEntry?.error || '',
        loadMainEarlier,
        retryMainHistory,
        subagent: subagentEntry?.agent || null,
        subagentLoading: Boolean(subagentEntry?.loading && !subagentEntry.entries.length),
        subagentHistoryLoading: subagentEntry?.loading ?? false,
        subagentHasEarlier: subagentEntry?.page?.hasMore ?? false,
        subagentEntries: subagentEntry?.entries ?? emptyEntries,
        subagentRequests: subagentEntry?.requests ?? emptyRequests,
        subagentEntriesError: subagentEntry?.error || '',
        loadSubagentEarlier,
        retrySubagentHistory,
        selectedAgentID: data.selectedId,
        outputStore,
        selectAgent: store.select,
    }
}
