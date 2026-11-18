import { mergeMessages, mergeRequests, type RequestActivities } from '../agent/history/entries'
import { activeRunId, type AgentView, type HistoryMessage, type HistoryPage, type SessionSnapshot } from '../../api'
import type { AgentEvent } from '../../api/events'
import type { StreamingOutputs } from '../agent/types'
import { applySessionEvent, clearHistory } from './applySessionEvent'

export type AgentEntry = {
    agent: AgentView
    entries: HistoryMessage[]
    requests: RequestActivities
    page?: Pick<HistoryPage, 'hasMore' | 'nextCursor'>
    loading: boolean
    error: string
}
export type SessionData = {
    session: Omit<SessionSnapshot, 'history'> | null
    selectedId: string
    ids: string[]
    byId: Record<string, AgentEntry>
    endedRuns: ReadonlySet<string>
    configRevision: number
}
export type SessionOutput = { streamingOutputs: StreamingOutputs }

/** 每个页面实例固定绑定一个 Session；普通数据和文本增量分别通知。 */
export function createSessionStore(sessionId: string) {
    let data: SessionData = {
        session: null,
        selectedId: sessionId,
        ids: [],
        byId: {},
        endedRuns: new Set(),
        configRevision: 0,
    }
    let output: SessionOutput = { streamingOutputs: {} }
    let buffered: AgentEvent[] = []
    let initialized = false
    const dataListeners = new Set<() => void>()
    const outputListeners = new Set<() => void>()
    const emitData = () => dataListeners.forEach((listener) => listener())
    const emitOutput = () => outputListeners.forEach((listener) => listener())

    const apply = (event: AgentEvent) => {
        const next = applySessionEvent(sessionId, data, output, event)
        data = next.data
        output = next.output
    }
    const receive = (event: AgentEvent) => {
        if (!initialized) {
            buffered.push(event)
            return
        }
        const previousData = data
        const previousOutput = output
        apply(event)
        if (data !== previousData) emitData()
        if (output !== previousOutput) emitOutput()
    }
    const updateEntry = (id: string, update: (current: AgentEntry) => AgentEntry) => {
        const current = data.byId[id]
        if (!current) return
        const updated = update(current)
        if (updated === current) return
        data = { ...data, byId: { ...data.byId, [id]: updated } }
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
        initialize({ history, ...session }: SessionSnapshot) {
            data = {
                ...data,
                selectedId: sessionId,
                endedRuns: new Set(),
                session,
                ids: [sessionId],
                byId: {
                    [sessionId]: {
                        ...historyAgent(sessionId, sessionId),
                        entries: history.messages,
                        requests: mergeRequests(new Map(), history.requests),
                        page: { hasMore: history.hasMore, nextCursor: history.nextCursor },
                    },
                },
            }
            initialized = true
            const previousOutput = output
            for (const event of buffered) apply(event)
            buffered = []
            emitData()
            if (output !== previousOutput) emitOutput()
        },
        select(id: string) {
            if (id === data.selectedId) return
            const previous = data.byId[data.selectedId]
            const byId = { ...data.byId }
            if (previous?.agent.Kind === 'subagent' && !activeRunId(previous.agent)) {
                byId[data.selectedId] = clearHistory(previous)
            }
            byId[id] ??= historyAgent(sessionId, id)
            data = { ...data, selectedId: id, byId }
            emitData()
        },
        startLoading(id: string) { updateEntry(id, (current) => ({ ...current, loading: true, error: '' })) },
        failed(id: string, error: string) {
            updateEntry(id, (current) => current.loading ? { ...current, loading: false, error } : current)
        },
        loaded(id: string, page: HistoryPage) {
            updateEntry(id, (current) => current.loading ? {
                ...current,
                entries: mergeMessages(page.messages, current.entries),
                requests: mergeRequests(current.requests, page.requests),
                page: { hasMore: page.hasMore, nextCursor: page.nextCursor },
                loading: false,
                error: '',
            } : current)
        },
        updateTitle(title: string) {
            if (data.session) {
                data = { ...data, session: { ...data.session, title } }
                emitData()
            }
        },
    }
}

function historyAgent(sessionId: string, agentId: string): AgentEntry {
    const main = agentId === sessionId
    return {
        agent: {
            ID: agentId, SessionID: sessionId, Kind: main ? 'main' : 'subagent',
            name: main ? '主代理' : '子代理', state: 'unknown',
        },
        entries: [], requests: new Map(), loading: false, error: '',
    }
}

export type SessionStore = ReturnType<typeof createSessionStore>
export type AgentOutputSource = Pick<SessionStore, 'getOutput' | 'subscribeOutput'>
