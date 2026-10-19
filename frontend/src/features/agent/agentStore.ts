import type { AgentEvent, AgentHistoryItem, AgentHistoryPage, AgentSnapshot } from '../../api'
import { clearDurablePendingUserMessages, clearDurableStreamingOutput, mergeStreamingEvent } from './streaming'
import type { PendingUserMessage, StreamingOutputs } from './types'
import { historyItemKey } from './history'

export type AgentEntry = {
    agent: AgentSnapshot
    history?: AgentHistoryItem[]
    nextCursor: string
    loading: boolean
    loadingOlder: boolean
    error: string
}

/** 查询快照和高频输出分别通知订阅者，数据仍由同一个会话 store 拥有。 */
export function createAgentStore() {
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
    const reconcile = (id: string) => {
        const entry = data.byId[id]
        if (!entry?.history) return
        updateOutput({
            ...output,
            streamingOutputs: clearDurableStreamingOutput(output.streamingOutputs, entry.agent, entry.history),
            pendingUserMessages: clearDurablePendingUserMessages(output.pendingUserMessages, entry.history),
            awaitingAgentId: output.awaitingAgentId === id && entry.agent.state !== 'executing' ? '' : output.awaitingAgentId,
        })
    }
    return {
        getData: () => data,
        getOutput: () => output,
        getSelectedAgentID: () => data.selectedId,
        subscribeData: (listener: () => void) => { dataListeners.add(listener); return () => { dataListeners.delete(listener) } },
        subscribeOutput: (listener: () => void) => { outputListeners.add(listener); return () => { outputListeners.delete(listener) } },
        reset() {
            data = { selectedId: '', ids: [], byId: {} }
            emitData()
            updateOutput({ ...output, streamingOutputs: {}, awaitingAgentId: '' })
        },
        setRoster(agents: AgentSnapshot[]) {
            const ids = agents.map((agent) => agent.id)
            const byId = Object.fromEntries(agents.map((agent) => {
                const cached = data.byId[agent.id]
                return [agent.id, cached ? { ...cached, loadingOlder: false } : {
                    agent, nextCursor: '', loading: false, loadingOlder: false, error: '',
                }]
            }))
            data = { ids, byId, selectedId: ids.includes(data.selectedId) ? data.selectedId : ids[0] || '' }
            emitData()
        },
        select(id: string) {
            data = { ...data, selectedId: id }
            emitData()
            updateOutput({ ...output, awaitingAgentId: '' })
        },
        startLoading(id: string) { updateEntry(id, (entry) => ({ ...entry, loading: entry.history === undefined })) },
        loaded(id: string, agent: AgentSnapshot, page?: AgentHistoryPage, error = '') {
            updateEntry(id, (entry) => ({
                ...entry, agent, loading: false, error,
                history: page ? mergeHistory(entry.history || [], page.items) : entry.history,
                nextCursor: !entry.history?.length && page ? page.nextCursor : entry.nextCursor,
            }))
            reconcile(id)
        },
        startOlder(id: string) { updateEntry(id, (entry) => ({ ...entry, loadingOlder: true, error: '' })) },
        olderLoaded(id: string, page?: AgentHistoryPage, error = '') {
            updateEntry(id, (entry) => ({
                ...entry, loadingOlder: false, error,
                history: page ? mergeHistory(entry.history || [], page.items) : entry.history,
                nextCursor: page?.nextCursor ?? entry.nextCursor,
            }))
            reconcile(id)
        },
        receive(event: AgentEvent) {
            if (!data.byId[event.agentId]) return
            if (event.kind === 'turn_started') {
                updateEntry(event.agentId, (entry) => ({ ...entry, agent: { ...entry.agent, state: entry.agent.state === 'pausing' ? 'pausing' : 'executing', currentExecutionId: event.executionId } }))
                return
            }
            const streamingOutputs = mergeStreamingEvent(output.streamingOutputs, event)
            if (streamingOutputs === output.streamingOutputs) return
            updateOutput({ ...output, streamingOutputs, awaitingAgentId: output.awaitingAgentId === event.agentId ? '' : output.awaitingAgentId })
        },
        beginInput({ requestId, sessionId, agentId, content }: Pick<PendingUserMessage, 'requestId' | 'sessionId' | 'agentId' | 'content'>) {
            const message: PendingUserMessage = { requestId, sessionId, agentId, content, executionId: '', at: new Date().toISOString() }
            // 运行中提交的是后续任务，不给当前回合额外显示等待响应。
            const awaitingAgentId = data.byId[agentId]?.agent.state === 'executing' ? output.awaitingAgentId : agentId
            updateOutput({ ...output, pendingUserMessages: [...output.pendingUserMessages, message], awaitingAgentId })
        },
        acceptInput(requestId: string, executionId: string) {
            updateOutput({ ...output, pendingUserMessages: output.pendingUserMessages.map((message) => message.requestId === requestId ? { ...message, executionId } : message) })
            for (const id of data.ids) reconcile(id)
        },
        rejectInput(requestId: string) {
            const message = output.pendingUserMessages.find((item) => item.requestId === requestId)
            updateOutput({ ...output, pendingUserMessages: output.pendingUserMessages.filter((item) => item.requestId !== requestId), awaitingAgentId: output.awaitingAgentId === message?.agentId ? '' : output.awaitingAgentId })
        },
        stopAwaiting() { updateOutput({ ...output, awaitingAgentId: '' }) },
    }
}

export type AgentStore = ReturnType<typeof createAgentStore>
export type AgentOutputSource = Pick<AgentStore, 'getOutput' | 'subscribeOutput'>

/** 续页和最新快照共用身份合并，重复加载不丢失已经展开的旧消息。 */
export function mergeHistory(current: AgentHistoryItem[], incoming: AgentHistoryItem[]): AgentHistoryItem[] {
    const values = new Map(current.map((item) => [historyItemKey(item), item]))
    for (const item of incoming) values.set(historyItemKey(item), item)
    return [...values.values()].sort((left, right) => (
        left.sequence !== undefined && right.sequence !== undefined && left.sequence !== right.sequence
            ? left.sequence - right.sequence : Date.parse(left.at) - Date.parse(right.at)
    ))
}
