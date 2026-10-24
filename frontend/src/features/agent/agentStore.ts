import type { AgentEvent, ContextEntry, AgentView } from '../../api'
import { mergeStreamingEvent } from './streaming'
import type { PendingUserMessage, StreamingOutputs } from './types'

export type AgentEntry = {
    agent: AgentView
    history?: ContextEntry[]
    loading: boolean
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
            data = { ...data, selectedId: id }
            emitData()
            updateOutput({ ...output, awaitingAgentId: '' })
        },
        startLoading(id: string) { updateEntry(id, (entry) => ({ ...entry, loading: entry.history === undefined })) },
        loaded(id: string, agent: AgentView, history?: ContextEntry[], error = '') {
            updateEntry(id, (entry) => ({ ...entry, agent, loading: false, error, history: history ?? entry.history }))
            // 完整历史加载成功后直接结束该执行的临时展示，不扫描正文做去重或修补。
            if (history) {
                const current = output.streamingOutputs[id]
                const ended = current && agent.currentExecutionId !== current.executionId
                const streamingOutputs = { ...output.streamingOutputs }
                if (ended) delete streamingOutputs[id]
                updateOutput({ ...output, streamingOutputs,
                    pendingUserMessages: output.pendingUserMessages.filter((message) => message.agentId !== id),
                    awaitingAgentId: output.awaitingAgentId === id && agent.state !== 'executing' ? '' : output.awaitingAgentId,
                })
            }
        },
        receive(event: AgentEvent) {
            if (!data.byId[event.agentId]) return
            if (event.entries?.length) {
                updateEntry(event.agentId, (entry) => ({ ...entry, history: [...(entry.history || []), ...event.entries!] }))
            }
            if (event.kind === 'turn_completed') {
                const current = output.streamingOutputs[event.agentId]
                if (current?.executionId === event.executionId) {
                    updateOutput({ ...output, streamingOutputs: { ...output.streamingOutputs, [event.agentId]: {
                        ...current, turns: current.turns.filter((turn) => turn.turnId !== event.turnId),
                    } } })
                }
                return
            }
            if (event.kind === 'turn_started') {
                updateEntry(event.agentId, (entry) => {
                    // 同一次执行收尾期间的迟到事件不能撤销已接受的暂停或停止。
                    const controlled = entry.agent.currentExecutionId === event.executionId &&
                        (entry.agent.state === 'paused' || entry.agent.state === 'interrupted')
                    return { ...entry, agent: { ...entry.agent, state: controlled ? entry.agent.state : 'executing', currentExecutionId: event.executionId } }
                })
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
