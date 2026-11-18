import { mergeMessages, mergeRequests, type RequestActivities } from '../agent/history/entries'
import type { AgentView, HistoryMessage, HistoryPage, SessionSnapshot, SessionUsageSummary } from '../../api'
import { isRequestEvent, type AgentEvent, type RequestEvent } from '../../api/events'
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
    session: Omit<SessionSnapshot, 'agents' | 'history' | 'usage'> | null
    selectedId: string
    ids: string[]
    byId: Record<string, AgentEntry>
    endedRuns: ReadonlySet<string>
    usage: SessionUsageSummary | null
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
        usage: null,
        configRevision: 0,
    }
    let output: SessionOutput = { streamingOutputs: {} }
    let buffered: AgentEvent[] = []
    let initialized = false
    const requestEvents = new Set<string>()
    const dataListeners = new Set<() => void>()
    const outputListeners = new Set<() => void>()
    const emitData = () => dataListeners.forEach((listener) => listener())
    const emitOutput = () => outputListeners.forEach((listener) => listener())

    const apply = (event: AgentEvent) => {
        // 快照按 Agent 标记已计入的日志前缀；分别接纳每次尝试的开始和结果。
        if (isRequestEvent(event)) {
            const key = JSON.stringify([event.agentId, event.runId, event.data.requestId, event.kind === 'model_request_started'])
            if (requestEvents.has(key)) return
            requestEvents.add(key)
            if (data.usage && event.data.historyEnd > (data.usage.historyEnds[event.agentId] ?? 0)) {
                data = { ...data, usage: accumulateUsage(data.usage, event) }
            }
        }
        const next = applySessionEvent(data, output, event)
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
        initialize({ agents, history, usage, ...session }: SessionSnapshot) {
            const byId: Record<string, AgentEntry> = {}
            const endedRuns = new Set<string>()
            for (const agent of agents) {
                const firstPage = agent.ID === session.id ? history : undefined
                byId[agent.ID] = {
                    agent,
                    entries: firstPage?.messages ?? [],
                    requests: mergeRequests(new Map(), firstPage?.requests ?? []),
                    page: firstPage && { hasMore: firstPage.hasMore, nextCursor: firstPage.nextCursor },
                    loading: false,
                    error: '',
                }
                if (agent.latestRun) endedRuns.add(agent.latestRun.ID)
            }
            data = {
                ...data,
                selectedId: byId[data.selectedId] ? data.selectedId : session.id,
                endedRuns,
                session,
                ids: agents.map((agent) => agent.ID),
                byId,
                usage,
            }
            initialized = true
            requestEvents.clear()
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
            if (previous?.agent.Kind === 'subagent' && !previous.agent.currentRunId) {
                byId[data.selectedId] = clearHistory(previous)
            }
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

function accumulateUsage(summary: SessionUsageSummary, event: RequestEvent): SessionUsageSummary {
    if (event.kind === 'model_request_started') return { ...summary, totalRequests: summary.totalRequests + 1 }
    const usage = event.data.result?.usage
    if (!usage) return summary
    return {
        ...summary,
        reportedRequests: summary.reportedRequests + 1,
        inputTokens: summary.inputTokens + (usage.inputTokens ?? 0),
        outputTokens: summary.outputTokens + (usage.outputTokens ?? 0),
        cacheReadInputTokens: summary.cacheReadInputTokens + (usage.cacheReadInputTokens ?? 0),
        cacheCreationInputTokens: summary.cacheCreationInputTokens + (usage.cacheCreationInputTokens ?? 0),
        inputReports: summary.inputReports + Number(usage.inputTokens !== undefined),
        outputReports: summary.outputReports + Number(usage.outputTokens !== undefined),
        cacheReadReports: summary.cacheReadReports + Number(usage.cacheReadInputTokens !== undefined),
        cacheCreationReports: summary.cacheCreationReports + Number(usage.cacheCreationInputTokens !== undefined),
    }
}

export type SessionStore = ReturnType<typeof createSessionStore>
export type AgentOutputSource = Pick<SessionStore, 'getOutput' | 'subscribeOutput'>
