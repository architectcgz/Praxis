import type { AgentEvent, Entry, AgentView } from '../../api'
import type { HistoryPage } from '../../api/agents'
import { mergeStreamingEvent } from './streaming'
import type { StreamingOutputs } from './types'

export type AgentEntry = {
    agent: AgentView
    entries?: Entry[]
    page?: Omit<HistoryPage, 'entries'>
    loading: boolean
    error: string
}

/** 查询快照和高频输出分别通知订阅者，数据仍由同一个会话 store 拥有。 */
export function createAgentStore() {
    let scope = { sessionId: '', branchId: '' }
    let data = { selectedId: '', ids: [] as string[], byId: {} as Record<string, AgentEntry> }
    let output = { streamingOutputs: {} as StreamingOutputs }
    const dataListeners = new Set<() => void>()
    const outputListeners = new Set<() => void>()
    const emitData = () => dataListeners.forEach((listener) => listener())
    const updateOutput = (streamingOutputs: StreamingOutputs) => {
        output = { streamingOutputs }
        outputListeners.forEach((listener) => listener())
    }
    const updateEntry = (id: string, update: (entry: AgentEntry) => AgentEntry) => {
        const current = data.byId[id]
        if (!current) return
        data = { ...data, byId: { ...data.byId, [id]: update(current) } }
        emitData()
    }
    return {
        getScope: () => scope,
        setScope(sessionId: string, branchId: string) {
            if (scope.sessionId !== sessionId || scope.branchId !== branchId) scope = { sessionId, branchId }
        },
        getData: () => data,
        getOutput: () => output,
        getSelectedAgentID: () => data.selectedId,
        subscribeData: (listener: () => void) => {
            dataListeners.add(listener)
            return () => { dataListeners.delete(listener) }
        },
        subscribeOutput: (listener: () => void) => {
            outputListeners.add(listener)
            return () => { outputListeners.delete(listener) }
        },
        reset() {
            scope = { sessionId: '', branchId: '' }
            data = { selectedId: '', ids: [], byId: {} }
            emitData()
            updateOutput({})
        },
        setRoster(agents: AgentView[]) {
            const ids = agents.map((agent) => agent.ID)
            const byId = Object.fromEntries(agents.map((agent) => {
                const cached = data.byId[agent.ID]
                return [agent.ID, cached ? { ...cached } : {
                    agent, loading: false, error: '',
                }]
            }))
            const selectedId = ids.includes(data.selectedId) ? data.selectedId : ids[0] || ''
            data = { ids, byId, selectedId }
            emitData()
        },
        select(id: string) {
            const previous = data.byId[data.selectedId]
            const byId = { ...data.byId }
            if (previous) byId[data.selectedId] = { ...previous, loading: false }
            if (previous?.agent.Kind === 'subagent' && !previous.agent.currentRunId) {
                byId[data.selectedId] = { ...previous, entries: undefined, page: undefined, error: '', loading: false }
            }
            data = { ...data, byId, selectedId: id }
            emitData()
        },
        startLoading(id: string) { updateEntry(id, (entry) => ({ ...entry, loading: true, error: '' })) },
        failed(id: string, error: string) { updateEntry(id, (entry) => ({ ...entry, loading: false, error })) },
        loaded(id: string, agent: AgentView, page?: HistoryPage, older = false) {
            updateEntry(id, (entry) => {
                let entries = entry.entries
                let pagination = entry.page
                if (page) {
                    const loadedEntries = entry.entries || []
                    entries = older ? mergeEntries(page.entries, loadedEntries) : mergeEntries(loadedEntries, page.entries)
                    const cursorPage = !older && entry.page ? entry.page : page
                    pagination = {
                        headEntryId: page.headEntryId,
                        hasMore: cursorPage.hasMore,
                        nextCursor: cursorPage.nextCursor,
                    }
                }
                return { ...entry, agent, entries, page: pagination, loading: false, error: '' }
            })
            // 正文接纳后清理已结束执行的临时输出；分页更新不改变仍在运行的输出。
            if (page) {
                const current = output.streamingOutputs[id]
                const ended = current && agent.currentRunId !== current.runId
                const streamingOutputs = { ...output.streamingOutputs }
                if (ended) delete streamingOutputs[id]
                updateOutput(streamingOutputs)
            }
        },
        receive(event: AgentEvent) {
            if (event.sessionId !== scope.sessionId || event.branchId !== scope.branchId) return
            if ((event.kind === 'model_request_started' || event.kind === 'model_request_finished') &&
                event.data.requestKind !== 'iteration') return
            if (event.kind === 'subagent_started') {
                const id = event.data.agent.ID
                data = {
                    ...data,
                    ids: data.ids.includes(id) ? data.ids : [...data.ids, id],
                    byId: { ...data.byId, [id]: { agent: event.data.agent, entries: event.data.entries, loading: false, error: '' } },
                }
                emitData()
                return
            }
            const agentId = event.agentId
            if (!agentId || !data.byId[agentId]) return
            if (event.kind === 'subagent_released') {
                const streamingOutputs = { ...output.streamingOutputs }
                delete streamingOutputs[agentId]
                updateOutput(streamingOutputs)
                if (data.selectedId !== agentId) updateEntry(agentId, (entry) => ({ ...entry, entries: undefined, page: undefined }))
                return
            }
            if (event.kind === 'run_started') {
                const run = event.data.run
                updateEntry(agentId, (entry) => ({
                    ...entry,
                    agent: {
                        ...entry.agent,
                        state: 'executing',
                        currentRunId: run.ID,
                        currentRun: run,
                        UpdatedAt: run.StartedAt,
                    },
                }))
                return
            }
            if (event.kind === 'run_interrupt_requested') {
                updateEntry(agentId, (entry) => {
                    if (entry.agent.currentRunId !== event.runId) return entry
                    return { ...entry, agent: { ...entry.agent, state: 'interrupted', UpdatedAt: event.data.appliedAt } }
                })
                return
            }
            if (event.kind === 'run_ended' || event.kind === 'request_canceled') {
                const { run, state, entries } = event.data
                updateEntry(agentId, (entry) => {
                    const retainHistory = entry.agent.Kind === 'main' || data.selectedId === agentId
                    const hasNewerRun = entry.agent.currentRunId && entry.agent.currentRunId !== run.ID
                    const agent = hasNewerRun ? entry.agent : {
                        ...entry.agent,
                        state,
                        currentRunId: '',
                        currentRun: null,
                        latestRun: run,
                        UpdatedAt: run.EndedAt,
                    }
                    return {
                        ...entry,
                        entries: retainHistory ? mergeEntries(entry.entries || [], entries) : undefined,
                        page: retainHistory ? entry.page : undefined,
                        agent,
                    }
                })
                const streamingOutputs = { ...output.streamingOutputs }
                if (streamingOutputs[agentId]?.runId === run.ID) delete streamingOutputs[agentId]
                updateOutput(streamingOutputs)
                return
            }
            if (event.kind === 'entries_appended' && event.data.entries.length) {
                updateEntry(agentId, (entry) => {
                    const retainHistory = entry.agent.currentRunId || entry.agent.Kind === 'main' || data.selectedId === agentId
                    const entries = retainHistory ? mergeEntries(entry.entries || [], event.data.entries) : undefined
                    return { ...entry, entries }
                })
            }
            if (event.kind === 'turn_completed') {
                const current = output.streamingOutputs[agentId]
                if (current?.runId === event.runId) {
                    const turns = current.turns.filter((turn) => turn.turnId !== event.turnId)
                    updateOutput({ ...output.streamingOutputs, [agentId]: { ...current, turns } })
                }
                return
            }
            if (event.kind === 'turn_started') {
                updateEntry(agentId, (entry) => {
                    // 同一次执行收尾期间的迟到事件不能撤销已接受的中断。
                    const interrupted = entry.agent.currentRunId === event.runId && entry.agent.state === 'interrupted'
                    return { ...entry, agent: { ...entry.agent, state: interrupted ? entry.agent.state : 'executing', currentRunId: event.runId } }
                })
                return
            }
            const streamingOutputs = mergeStreamingEvent(output.streamingOutputs, agentId, event)
            if (streamingOutputs !== output.streamingOutputs) updateOutput(streamingOutputs)
        },
    }
}

function mergeEntries(current: Entry[], incoming: Entry[]): Entry[] {
    return [...new Map([...current, ...incoming].map((entry) => [entry.id, entry])).values()]
}

export type AgentOutputSource = Pick<ReturnType<typeof createAgentStore>, 'getOutput' | 'subscribeOutput'>
