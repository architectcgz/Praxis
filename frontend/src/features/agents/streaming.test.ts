import { describe, expect, it, vi } from 'vitest'
import { agent, message } from '../../test/fixtures'
import { createAgentStore } from './agentStore'
import { loadLatestHistory } from './history'
import { clearDurableStreamingOutput, mergeStreamingEvent, projectStreamingOutput } from './streaming'
import type { AgentEvent } from '../../api'

const event = (kind: AgentEvent['kind'], turnId = 'turn-1', rest: Partial<AgentEvent> = {}): AgentEvent => ({ kind, agentId: 'a1', taskId: 't1', turnId, ...rest })

describe('历史与实时输出归并', () => {
    it('已持久化的轮次不重复显示，下一轮增量继续保留', () => {
        let outputs = mergeStreamingEvent({}, event('text_delta', 'turn-1', { text: '已保存' }))
        outputs = mergeStreamingEvent(outputs, event('text_delta', 'turn-2', { text: '下一轮' }))
        const history = [message(1)]
        const projected = projectStreamingOutput(outputs.a1, history)!
        expect(projected.turns.map((turn) => turn.turnId)).toEqual(['turn-2'])
        expect(outputs.a1.turns).toHaveLength(2)
    })

    it('多工具回合不会因第一条工具结果落盘就清掉其余实时结果', () => {
        let outputs = mergeStreamingEvent({}, event('tool_result', 'turn-1', { callId: 'one', name: 'bash', result: 'one' }))
        outputs = mergeStreamingEvent(outputs, event('tool_result', 'turn-1', { callId: 'two', name: 'bash', result: 'two' }))
        const ended = agent('a1', 's1', { tasks: [{ id: 't1', status: 'ended', outcome: 'completed', failureCode: '', failureMessage: '', createdAt: '', startedAt: '', endedAt: '' }] })
        const history = [message(1, { blocks: [{ kind: 'tool_call', callId: 'one' }, { kind: 'tool_call', callId: 'two' }] }), message(2, { role: 'tool', blocks: [{ kind: 'tool_result', callId: 'one' }] })]
        expect(clearDurableStreamingOutput(outputs, ended, history).a1).toBeDefined()
        history.push(message(3, { role: 'tool', blocks: [{ kind: 'tool_result', callId: 'two' }] }))
        expect(clearDurableStreamingOutput(outputs, ended, history).a1).toBeUndefined()
    })

    it('失败时保留尚未落盘的部分文本', () => {
        const outputs = mergeStreamingEvent({}, event('text_delta', 'turn-1', { text: '部分输出' }))
        const ended = agent('a1', 's1', { tasks: [{ id: 't1', status: 'ended', outcome: 'failed', failureCode: 'task_provider_error', failureMessage: '', createdAt: '', startedAt: '', endedAt: '' }] })
        expect(clearDurableStreamingOutput(outputs, ended, [{ kind: 'task', at: '', task: ended.tasks[0] }]).a1).toBeDefined()
    })

    it('文本 delta 只通知输出订阅，不通知工作区数据订阅', () => {
        const store = createAgentStore()
        store.setRoster([agent()])
        const dataListener = vi.fn(), outputListener = vi.fn()
        store.subscribeData(dataListener)
        store.subscribeOutput(outputListener)
        const snapshot = store.getData()
        store.receive(event('text_delta', 'turn-1', { text: 'delta' }))
        expect(dataListener).not.toHaveBeenCalled()
        expect(store.getData()).toBe(snapshot)
        expect(outputListener).toHaveBeenCalledOnce()
    })

    it('加载旧页后刷新最新页，不丢失已加载记录或回退旧页游标', () => {
        const store = createAgentStore()
        store.setRoster([agent()])
        store.loaded('a1', agent(), { items: [message(201), message(202)], nextCursor: 'message:201' })
        store.olderLoaded('a1', { items: [message(199), message(200)], nextCursor: 'message:199' })
        store.loaded('a1', agent(), { items: [message(202), message(203)], nextCursor: 'message:202' })
        expect(store.getData().byId.a1.history?.map((item) => item.sequence)).toEqual([199, 200, 201, 202, 203])
        expect(store.getData().byId.a1.nextCursor).toBe('message:199')
    })

    it('刷新跨过多页时补齐缓存与最新页之间的消息', async () => {
        const read = vi.fn().mockResolvedValueOnce({ items: [message(601), message(602)], nextCursor: 'message:601' })
            .mockResolvedValueOnce({ items: [message(401), message(600)], nextCursor: 'message:401' })
            .mockResolvedValueOnce({ items: [message(201), message(400)], nextCursor: 'message:201' })
        const page = await loadLatestHistory('a1', [message(200), message(201)], read)
        expect(read).toHaveBeenCalledTimes(3)
        expect(page.items.map((item) => item.sequence)).toEqual([201, 400, 401, 600, 601, 602])
    })
})
