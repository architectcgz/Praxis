import type { AgentEvent, Entry, AgentView } from '../../api'
import type { HistoryPage } from '../../api/agents'
import { mergeStreamingEvent } from './streaming'
import type { PendingUserMessage, StreamingOutputs } from './types'

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
    let output = { streamingOutputs: {} as StreamingOutputs, pendingUserMessages: [] as PendingUserMessage[], awaitingAgentId: '' }
    const dataListeners = new Set<() => void>()
    const outputListeners = new Set<() => void>()
    const emitData = () => dataListeners.forEach((listener) => listener())
    const updateOutput = (next: typeof output) => { output = next; outputListeners.forEach((listener) => listener()) }
    const updateEntry = (id: string, update: (entry: AgentEntry) => AgentEntry) => {
        const current = data.byId[id]
        if (!current) return
        data = { ...data, byId: { ...data.byId, [id]: update(current) } }
        emitData()
    }
    return {
        getScope: () => scope,
        setScope(sessionId: string, branchId: string) { if (scope.sessionId !== sessionId || scope.branchId !== branchId) scope = { sessionId, branchId } },
        getData: () => data,
        getOutput: () => output,
        getSelectedAgentID: () => data.selectedId,
        subscribeData: (listener: () => void) => { dataListeners.add(listener); return () => { dataListeners.delete(listener) } },
        subscribeOutput: (listener: () => void) => { outputListeners.add(listener); return () => { outputListeners.delete(listener) } },
        reset() {
            scope = { sessionId: '', branchId: '' }
            data = { selectedId: '', ids: [], byId: {} }
            emitData()
            updateOutput({ streamingOutputs: {}, pendingUserMessages: [], awaitingAgentId: '' })
        },
        setRoster(agents: AgentView[]) {
            const ids = agents.map((agent) => agent.ID)
            const byId = Object.fromEntries(agents.map((agent) => {
                const cached = data.byId[agent.ID]
                return [agent.ID, cached ? { ...cached } : {
                    agent, loading: false, error: '',
                }]
            }))
            data = { ids, byId, selectedId: ids.includes(data.selectedId) ? data.selectedId : ids[0] || '' }
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
            updateOutput({ ...output, awaitingAgentId: '' })
        },
        startLoading(id: string) { updateEntry(id, (entry) => ({ ...entry, loading: true, error: '' })) },
        failed(id: string, error: string) { updateEntry(id, (entry) => ({ ...entry, loading: false, error })) },
        loaded(id: string, agent: AgentView, page?: HistoryPage, older = false) {
            const entries = page?.entries
            const pagination = page ? { headEntryId: page.headEntryId, hasMore: page.hasMore, nextCursor: page.nextCursor } : undefined
            updateEntry(id, (entry) => ({ ...entry, agent, loading: false, error: '',
                entries: entries ? older ? mergeEntries(entries, entry.entries || []) : mergeEntries(entry.entries || [], entries) : entry.entries,
                page: pagination ? older || !entry.page ? pagination : { ...pagination, hasMore: entry.page.hasMore, nextCursor: entry.page.nextCursor } : entry.page,
            }))
            // 完整条目加载成功后直接结束该执行的临时展示，不扫描正文做去重或修补。
            if (entries) {
                const current = output.streamingOutputs[id]
                const ended = current && agent.currentRunId !== current.runId
                const streamingOutputs = { ...output.streamingOutputs }
                if (ended) delete streamingOutputs[id]
                updateOutput({ ...output, streamingOutputs,
                    pendingUserMessages: output.pendingUserMessages.filter((message) => message.agentId !== id),
                    awaitingAgentId: output.awaitingAgentId === id && agent.state !== 'executing' ? '' : output.awaitingAgentId,
                })
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
                updateOutput({ ...output, streamingOutputs, awaitingAgentId: output.awaitingAgentId === agentId ? '' : output.awaitingAgentId })
                if (data.selectedId !== agentId) updateEntry(agentId, (entry) => ({ ...entry, entries: undefined, page: undefined }))
                return
            }
            if (event.kind === 'run_started') {
                const run = event.data.run
                updateEntry(agentId, (entry) => ({ ...entry, agent: {
                    ...entry.agent, state: 'executing', currentRunId: run.ID,
                    currentRun: run, UpdatedAt: run.StartedAt,
                } }))
                return
            }
            if (event.kind === 'run_interrupt_requested') {
                updateEntry(agentId, (entry) => entry.agent.currentRunId === event.runId ? {
                    ...entry, agent: { ...entry.agent, state: 'interrupted', UpdatedAt: event.data.appliedAt },
                } : entry)
                return
            }
            if (event.kind === 'run_ended' || event.kind === 'request_canceled') {
                const { run, state, entries } = event.data
                updateEntry(agentId, (entry) => ({
                    ...entry,
                    entries: entry.agent.Kind === 'main' || data.selectedId === agentId ? mergeEntries(entry.entries || [], entries) : undefined,
                    page: entry.agent.Kind === 'main' || data.selectedId === agentId ? entry.page : undefined,
                    agent: entry.agent.currentRunId && entry.agent.currentRunId !== run.ID ? entry.agent : {
                        ...entry.agent, state, currentRunId: '', currentRun: null,
                        latestRun: run, UpdatedAt: run.EndedAt,
                    },
                }))
                const streamingOutputs = { ...output.streamingOutputs }
                if (streamingOutputs[agentId]?.runId === run.ID) delete streamingOutputs[agentId]
                updateOutput({ ...output, streamingOutputs, awaitingAgentId: output.awaitingAgentId === agentId ? '' : output.awaitingAgentId })
                return
            }
            if (event.kind === 'entries_appended' && event.data.entries.length) {
                const entries = event.data.entries
                updateEntry(agentId, (entry) => ({ ...entry, entries: entry.agent.currentRunId || entry.agent.Kind === 'main' || data.selectedId === agentId ? mergeEntries(entry.entries || [], entries) : undefined }))
            }
            if (event.kind === 'turn_completed') {
                const current = output.streamingOutputs[agentId]
                if (current?.runId === event.runId) {
                    updateOutput({ ...output, streamingOutputs: { ...output.streamingOutputs, [agentId]: {
                        ...current, turns: current.turns.filter((turn) => turn.turnId !== event.turnId),
                    } } })
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
            if (streamingOutputs === output.streamingOutputs) return
            updateOutput({ ...output, streamingOutputs, awaitingAgentId: output.awaitingAgentId === agentId ? '' : output.awaitingAgentId })
        },
        beginInput({ requestId, sessionId, agentId, content }: Pick<PendingUserMessage, 'requestId' | 'sessionId' | 'agentId' | 'content'>) {
            const message: PendingUserMessage = { requestId, sessionId, agentId, content, runId: '', at: new Date().toISOString() }
            // 运行中提交的是后续任务，不给当前回合额外显示等待响应。
            const awaitingAgentId = data.byId[agentId]?.agent.state === 'executing' ? output.awaitingAgentId : agentId
            updateOutput({ ...output, pendingUserMessages: [...output.pendingUserMessages, message], awaitingAgentId })
        },
        acceptInput(requestId: string, runId: string) {
            updateOutput({ ...output, pendingUserMessages: output.pendingUserMessages.map((message) => message.requestId === requestId ? { ...message, runId } : message) })
        },
        rejectInput(requestId: string) {
            const message = output.pendingUserMessages.find((item) => item.requestId === requestId)
            updateOutput({ ...output, pendingUserMessages: output.pendingUserMessages.filter((item) => item.requestId !== requestId), awaitingAgentId: output.awaitingAgentId === message?.agentId ? '' : output.awaitingAgentId })
        },
        stopAwaiting() { updateOutput({ ...output, awaitingAgentId: '' }) },
    }
}

function mergeEntries(current: Entry[], incoming: Entry[]): Entry[] {
    return [...new Map([...current, ...incoming].map((entry) => [entry.id, entry])).values()]
}

export type AgentStore = ReturnType<typeof createAgentStore>
export type AgentOutputSource = Pick<AgentStore, 'getOutput' | 'subscribeOutput'>
