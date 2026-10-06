import type { AgentEvent, AgentHistoryItem, AgentSnapshot } from '../../api'
import type { PendingUserMessage, StreamingOutput, StreamingOutputs } from './types'

/**
 * 按 Agent、task 和 turn 合并实时事件，不修改输入对象。
 * 仅相邻同类文本增量拼接；终态清除等待标记但保留尚未落盘的输出。
 */
export function mergeStreamingEvent(outputs: StreamingOutputs, event: AgentEvent): StreamingOutputs {
    const previous = outputs[event.agentId]
    const output: StreamingOutput = previous?.taskId === event.taskId
        ? previous
        : { taskId: event.taskId, turns: [], error: '' }
    switch (event.kind) {
        case 'text_delta':
        case 'thinking_delta':
        case 'provider_waiting':
        case 'tool_call':
        case 'tool_result': {
            const turnId = event.turnId || ''
            const turns = [...output.turns]
            const index = turns.findIndex((item) => item.turnId === turnId)
            const previousTurn = turns[index] || { turnId, events: [] }
            const events = [...previousTurn.events]
            const last = events[events.length - 1]
            if ((event.kind === 'text_delta' || event.kind === 'thinking_delta') && last?.kind === event.kind) {
                events[events.length - 1] = { ...last, text: (last.text || '') + (event.text || '') }
            } else {
                events.push(event)
            }
            const nextTurn = { ...previousTurn, events }
            if (index < 0) {
                turns.push(nextTurn)
            } else {
                turns[index] = nextTurn
            }
            return { ...outputs, [event.agentId]: { ...output, turns } }
        }
        case 'turn_completed': {
            const turnId = event.turnId || ''
            const index = output.turns.findIndex((item) => item.turnId === turnId)
            if (index < 0) {
                return outputs
            }
            const turns = [...output.turns]
            turns[index] = {
                ...turns[index],
                events: turns[index].events.filter((item) => item.kind !== 'provider_waiting'),
            }
            return { ...outputs, [event.agentId]: { ...output, turns } }
        }
        case 'request_canceled':
        case 'task_ended':
        case 'error':
            if (event.kind !== 'error' && previous?.taskId !== event.taskId) return outputs
            return { ...outputs, [event.agentId]: {
                ...output,
                turns: output.turns.map((turn) => ({ ...turn, events: turn.events.filter((item) => item.kind !== 'provider_waiting') })),
                error: event.kind === 'error' ? event.error || '' : output.error,
            } }
        default:
            return outputs
    }
}

/** 仅在执行已结算且历史包含对应输出时移除流式副本；工具输出还需等待工具记录落盘。 */
export function clearDurableStreamingOutput(outputs: StreamingOutputs, agent: AgentSnapshot, history: AgentHistoryItem[]): StreamingOutputs {
    const output = outputs[agent.id]
    if (!output) {
        return outputs
    }
    const ended = agent.tasks.some((task) => task.id === output.taskId && task.status === 'ended')
    const persisted = history.some((item) => (
        item.message?.role === 'assistant' && item.message.taskId === output.taskId
    ) || item.task?.id === output.taskId)
    const hasToolOutput = output.turns.some((turn) => turn.events.some((event) => event.kind === 'tool_call' || event.kind === 'tool_result'))
    const persistedToolOutput = history.some((item) => (
        item.message?.taskId === output.taskId &&
        item.message.blocks?.some((block) => block.kind === 'tool_call' || block.kind === 'tool_result')
    ))
    if (!ended || !persisted || hasToolOutput && !persistedToolOutput) {
        return outputs
    }
    const remaining = { ...outputs }
    delete remaining[agent.id]
    return remaining
}

/** 按落盘用户消息的 task ID 去重；尚未收到执行回执或其他执行的乐观消息继续保留。 */
export function clearDurablePendingUserMessages(pending: PendingUserMessage[], history: AgentHistoryItem[]): PendingUserMessage[] {
    if (pending.length === 0) {
        return pending
    }
    const persistedTaskIDs = new Set<string>()
    for (const item of history) {
        if (item.message?.role === 'user' && item.message.taskId) {
            persistedTaskIDs.add(item.message.taskId)
        }
    }
    if (persistedTaskIDs.size === 0) {
        return pending
    }
    const remaining = pending.filter((message) => message.taskId === '' || !persistedTaskIDs.has(message.taskId))
    return remaining.length === pending.length ? pending : remaining
}
