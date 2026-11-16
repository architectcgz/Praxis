import type { AgentEvent } from '../../api'
import type { StreamingOutput, StreamingOutputs } from '../agent/types'

/**
 * 按 Agent、Run 和 Turn 合并实时事件，不修改输入对象。
 * 仅相邻同类文本增量拼接；回合与执行终态由 store 接纳正文并清理输出。
 */
export function mergeStreamingEvent(outputs: StreamingOutputs, agentId: string, event: AgentEvent): StreamingOutputs {
    if (!('runId' in event)) return outputs
    const previous = outputs[agentId]
    const output: StreamingOutput = previous?.runId === event.runId
        ? previous
        : { runId: event.runId, turns: [], error: '' }
    switch (event.kind) {
        case 'text_delta':
        case 'thinking_delta':
        case 'model_request_started':
        case 'model_request_completed':
        case 'model_request_failed':
        case 'model_request_interrupted':
        case 'tool_call':
        case 'tool_started':
        case 'tool_approval_required':
        case 'tool_result': {
            if (!event.turnId) return outputs
            const turnId = event.turnId
            const turns = [...output.turns]
            const index = turns.findIndex((item) => item.turnId === turnId)
            const previousTurn = turns[index] || { turnId, events: [] }
            const events = event.kind === 'model_request_started' && event.data.requestKind === 'iteration' ? [] : [...previousTurn.events]
            const last = events[events.length - 1]
            if ((event.kind === 'text_delta' || event.kind === 'thinking_delta') && last?.kind === event.kind) {
                events[events.length - 1] = { ...event, data: { text: last.data.text + event.data.text } }
            } else {
                events.push(event)
            }
            const nextTurn = { ...previousTurn, events }
            if (index < 0) {
                turns.push(nextTurn)
            } else {
                turns[index] = nextTurn
            }
            return { ...outputs, [agentId]: { ...output, turns } }
        }
        case 'error':
            return { ...outputs, [agentId]: {
                ...output,
                turns: output.turns.map((turn) => ({ ...turn, events: turn.events.filter((item) => (
                    item.kind !== 'model_request_started' && item.kind !== 'tool_approval_required'
                )) })),
                error: event.data.message,
            } }
        default:
            return outputs
    }
}
