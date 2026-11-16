import { mergeEntries } from '../agent/history/entries'
import type { AgentView, Entry, SessionSnapshot } from '../../api'
import type { HistoryPage } from '../../api/agents'
import type { AgentEvent } from '../../api/events'
import type { StreamingOutputs } from '../agent/types'
import { applySessionEvent } from './applySessionEvent'

export type AgentEntry = {
    agent: AgentView
    entries?: Entry[]
    page?: Omit<HistoryPage, 'entries'>
    loading: boolean
    error: string
}
export type SessionData = {
    session: SessionSnapshot | null
    selectedId: string
    ids: string[]
    byId: Record<string, AgentEntry>
    runAgents: Record<string, string>
    endedRuns: ReadonlySet<string>
    usageRevision: number
    configRevision: number
    eventError: string
}
export type SessionOutput = { streamingOutputs: StreamingOutputs }
/** 每个页面实例固定绑定一个 Session；普通数据和文本增量分别通知。 */
export function createSessionStore(sessionId: string) {
    let data: SessionData = { session: null, selectedId: sessionId, ids: [], byId: {}, runAgents: {}, endedRuns: new Set(), usageRevision: 0, configRevision: 0, eventError: '' }
    let output: SessionOutput = { streamingOutputs: {} }
    let buffered: AgentEvent[] = []
    let initialized = false
    const dataListeners = new Set<() => void>()
    const outputListeners = new Set<() => void>()
    const emitData = () => dataListeners.forEach((listener) => listener())
    const emitOutput = () => outputListeners.forEach((listener) => listener())
    const receive = (event: AgentEvent) => {
        if (!initialized) { buffered.push(event); return }
        const next = applySessionEvent(data, output, event)
        const dataChanged = data !== next.data
        const outputChanged = output !== next.output
        data = next.data
        output = next.output
        if (dataChanged) emitData()
        if (outputChanged) emitOutput()
    }
    const updateEntry = (id: string, update: (current: AgentEntry) => AgentEntry) => {
        const current = data.byId[id]
        if (!current) return
        data = { ...data, byId: { ...data.byId, [id]: update(current) } }
        emitData()
    }
    return {
        sessionId,
        getData: () => data,
        getOutput: () => output,
        getSelectedAgentID: () => data.selectedId,
        subscribeData(listener: () => void) { dataListeners.add(listener); return () => { dataListeners.delete(listener) } },
        subscribeOutput(listener: () => void) { outputListeners.add(listener); return () => { outputListeners.delete(listener) } },
        receive,
        beginLoad() { initialized = false },
        initialize(snapshot: SessionSnapshot) {
            // 初始化期间先保存事件，快照接纳后依顺序重放，旧快照不能吞掉新执行。
            const byId: Record<string, AgentEntry> = {}
            for (const agent of snapshot.agents) {
                const page = snapshot.entries[agent.ID]
                byId[agent.ID] = { agent, entries: page?.entries, page, loading: false, error: '' }
            }
            const runAgents = { ...data.runAgents }
            const endedRuns = new Set(data.endedRuns)
            for (const agent of snapshot.agents) {
                for (const run of [agent.currentRun,agent.latestRun]) if (run) { runAgents[run.ID]=agent.ID; if (run.Status==='ended') endedRuns.add(run.ID) }
            }
            data = { ...data, runAgents, endedRuns, session: snapshot, ids: snapshot.agents.map((agent) => agent.ID), byId }
            initialized = true
            const pending = buffered
            buffered = []
            for (const event of pending) receive(event)
            emitData()
        },
        select(id: string) {
            if (id===data.selectedId) return
            const previous=data.byId[data.selectedId]
            const byId={...data.byId}
            if (previous?.agent.Kind==='subagent' && !previous.agent.currentRunId) byId[data.selectedId]={...previous,entries:undefined,page:undefined,error:'',loading:false}
            data = { ...data, selectedId: id,byId }; emitData()
        },
        startLoading(id: string) { updateEntry(id, (current) => ({ ...current, loading: true, error: '' })) },
        failed(id: string, error: string) { updateEntry(id, (current) => ({ ...current, loading: false, error })) },
        loaded(id: string, page: HistoryPage, older: boolean) {
            updateEntry(id, (current) => ({
                ...current,
                entries: older ? mergeEntries(page.entries, current.entries || []) : mergeEntries(current.entries || [], page.entries),
                page: { headEntryId: page.headEntryId, hasMore: page.hasMore, nextCursor: page.nextCursor },
                loading: false,
                error: '',
            }))
        },
        updateTitle(title: string) { if (data.session) { data = { ...data, session: { ...data.session, title } }; emitData() } },
    }
}
export type SessionStore = ReturnType<typeof createSessionStore>
export type AgentOutputSource = Pick<SessionStore, 'getOutput' | 'subscribeOutput'>
