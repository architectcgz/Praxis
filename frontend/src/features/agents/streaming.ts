import type { AgentEvent, AgentHistoryItem, AgentSnapshot } from '../../api'
import type { PendingUserMessage, StreamingOutput, StreamingOutputs } from './types'

/**
 * 按 Agent、turn 和 step 合并实时事件，不修改输入对象。
 * 仅相邻同类文本增量拼接；终态清除等待标记但保留尚未落盘的输出。
 */
export function mergeStreamingEvent(outputs: StreamingOutputs, event: AgentEvent): StreamingOutputs {
    const previous = outputs[event.agentId]
    const output: StreamingOutput = previous?.turnId === event.turnId
        ? previous
        : { turnId: event.turnId, steps: [], error: '' }
    switch (event.kind) {
        case 'text_delta':
        case 'thinking_delta':
        case 'provider_waiting':
        case 'tool_call':
        case 'tool_result': {
            const step = event.step || 1
            const steps = [...output.steps]
            const index = steps.findIndex((item) => item.step === step)
            const previousStep = steps[index] || { step, events: [] }
            const events = [...previousStep.events]
            const last = events[events.length - 1]
            if ((event.kind === 'text_delta' || event.kind === 'thinking_delta') && last?.kind === event.kind) {
                events[events.length - 1] = { ...last, text: (last.text || '') + (event.text || '') }
            } else {
                events.push(event)
            }
            const nextStep = { ...previousStep, events }
            if (index < 0) {
                steps.push(nextStep)
            } else {
                steps[index] = nextStep
            }
            return { ...outputs, [event.agentId]: { ...output, steps } }
        }
        case 'step_completed': {
            const step = event.step || 1
            const index = output.steps.findIndex((item) => item.step === step)
            if (index < 0) {
                return outputs
            }
            const steps = [...output.steps]
            steps[index] = {
                ...steps[index],
                events: steps[index].events.filter((item) => item.kind !== 'provider_waiting'),
            }
            return { ...outputs, [event.agentId]: { ...output, steps } }
        }
        case 'request_canceled':
        case 'turn_ended':
        case 'error':
            if (event.kind !== 'error' && previous?.turnId !== event.turnId) return outputs
            return { ...outputs, [event.agentId]: {
                ...output,
                steps: output.steps.map((step) => ({ ...step, events: step.events.filter((item) => item.kind !== 'provider_waiting') })),
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
    const ended = agent.turns.some((turn) => turn.id === output.turnId && turn.status === 'ended')
    const persisted = history.some((item) => (
        item.message?.role === 'assistant' && item.message.turnId === output.turnId
    ) || item.turn?.id === output.turnId)
    const hasToolOutput = output.steps.some((step) => step.events.some((event) => event.kind === 'tool_call' || event.kind === 'tool_result'))
    const persistedToolOutput = history.some((item) => (
        item.message?.turnId === output.turnId &&
        item.message.blocks?.some((block) => block.kind === 'tool_call' || block.kind === 'tool_result')
    ))
    if (!ended || !persisted || hasToolOutput && !persistedToolOutput) {
        return outputs
    }
    const remaining = { ...outputs }
    delete remaining[agent.id]
    return remaining
}

/** 按落盘用户消息的 turn ID 去重；尚未收到执行回执或其他执行的乐观消息继续保留。 */
export function clearDurablePendingUserMessages(pending: PendingUserMessage[], history: AgentHistoryItem[]): PendingUserMessage[] {
    if (pending.length === 0) {
        return pending
    }
    const persistedTurnIDs = new Set<string>()
    for (const item of history) {
        if (item.message?.role === 'user' && item.message.turnId) {
            persistedTurnIDs.add(item.message.turnId)
        }
    }
    if (persistedTurnIDs.size === 0) {
        return pending
    }
    const remaining = pending.filter((message) => message.turnId === '' || !persistedTurnIDs.has(message.turnId))
    return remaining.length === pending.length ? pending : remaining
}
