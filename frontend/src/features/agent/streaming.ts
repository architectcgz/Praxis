import type { AgentEvent } from '../../api'
import type { StreamingOutput, StreamingOutputs } from './types'

/**
 * 按 Agent、task 和 turn 合并实时事件，不修改输入对象。
 * 仅相邻同类文本增量拼接；终态清除等待标记但保留尚未落盘的输出。
 */
export function mergeStreamingEvent(outputs: StreamingOutputs, event: AgentEvent): StreamingOutputs {
    const previous = outputs[event.agentId]
    const output: StreamingOutput = previous?.executionId === event.executionId
        ? previous
        : { executionId: event.executionId, turns: [], error: '' }
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
        case 'execution_ended':
        case 'error':
            if (event.kind !== 'error' && previous?.executionId !== event.executionId) return outputs
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
