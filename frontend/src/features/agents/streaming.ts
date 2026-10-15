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
        case 'tool_approval_required':
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
                turns: output.turns.map((turn) => ({ ...turn, events: turn.events.filter((item) => (
                    item.kind !== 'provider_waiting' && item.kind !== 'tool_approval_required'
                )) })),
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
    const fullyPersisted = output.turns.every((turn) => turn.events.every((event) => {
        if (event.kind === 'text_delta' || event.kind === 'thinking_delta') return hasAssistantTurn(history, output.taskId, turn.turnId)
        if (event.kind === 'tool_call' || event.kind === 'tool_result') return history.some((item) => (
            item.message?.taskId === output.taskId && item.message.blocks?.some((block) => block.kind === event.kind && block.callId === event.callId)
        ))
        return true
    }))
    if (!ended || !persisted || !fullyPersisted) {
        return outputs
    }
    const remaining = { ...outputs }
    delete remaining[agent.id]
    return remaining
}

/** 持久化轮次拥有正文与调用卡片；未落盘的工具结果由调用卡片消费。 */
export function projectStreamingOutput(output: StreamingOutput | null, history: AgentHistoryItem[]): StreamingOutput | null {
    if (!output) return null
    return {
        ...output,
        turns: output.turns.flatMap((turn) => {
            if (!hasAssistantTurn(history, output.taskId, turn.turnId)) return [turn]
            const events = turn.events.filter((event) => {
                if (event.kind === 'text_delta' || event.kind === 'thinking_delta' || event.kind === 'provider_waiting') return false
                return !history.some((item) => item.message?.taskId === output.taskId && item.message.blocks?.some((block) => (
                    block.kind === 'tool_call' && block.callId === event.callId
                )))
            })
            return events.length > 0 ? [{ ...turn, events }] : []
        }),
    }
}

function hasAssistantTurn(history: AgentHistoryItem[], taskId: string, turnId: string): boolean {
    return history.some((item) => item.message?.taskId === taskId && item.message.role === 'assistant' && item.message.id === `assistant:${turnId}`)
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
