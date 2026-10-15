import { act, renderHook, waitFor } from '@testing-library/react'
import { StrictMode, useState } from 'react'
import { beforeEach, describe, expect, it, vi } from 'vitest'
import type { AgentEvent, AgentHistoryPage, SessionSnapshot } from '../../api'
import { agent, deferred, message, session, summary } from '../../test/fixtures'
import { useWorkspaceData } from './useWorkspaceData'
import { useSessionCommands } from '../sessions/useSessionCommands'

const api = vi.hoisted(() => ({
    getSession: vi.fn(), listProjects: vi.fn(), listModels: vi.fn(), listSessions: vi.fn(), getAgent: vi.fn(), listAgentHistory: vi.fn(),
    createSession: vi.fn(), deleteSession: vi.fn(), renameSession: vi.fn(), revealSessionFile: vi.fn(),
    listeners: new Set<(event: AgentEvent) => void>(),
}))
vi.mock('../../api', () => ({ ...api, API_ERROR_CODES: { modelNotConfigured: 'model.not_configured' }, isBindingAvailable: () => true,
    subscribeAgentEvents: (listener: (event: AgentEvent) => void) => { api.listeners.add(listener); return () => api.listeners.delete(listener) },
}))
vi.mock('../../shared/errors', () => ({ apiErrorCode: () => undefined, readableError: (error: Error) => error.message }))

beforeEach(() => {
    vi.resetAllMocks()
    api.listeners.clear()
    window.localStorage.clear()
    api.listProjects.mockResolvedValue([{ id: 'p1', name: 'Project', defaultWorkspaceId: 'w1', path: '/project', state: 'ready' }])
    api.listModels.mockResolvedValue([])
    api.listSessions.mockResolvedValue([summary()])
    api.getSession.mockImplementation(async (id: string) => session(id))
    api.getAgent.mockImplementation(async (id: string) => agent(id, id.slice(2)))
    api.listAgentHistory.mockResolvedValue({ items: [], nextCursor: '' })
})

describe('会话异步归属', () => {
    it('加载期间再次选择同一个会话保留原请求', async () => {
        const pending = deferred<SessionSnapshot>()
        api.getSession.mockReturnValue(pending.promise)
        const { result } = renderHook(useWorkspaceData)
        await waitFor(() => expect(api.getSession).toHaveBeenCalledOnce())
        act(() => result.current.selectSession('s1'))
        await act(async () => pending.resolve(session()))
        await waitFor(() => expect(result.current.agent?.id).toBe('a-s1'))
        expect(api.getSession).toHaveBeenCalledOnce()
        expect(result.current.sessionLoading).toBe(false)
    })

    it('失败后再次点击当前会话会重新加载并清除错误', async () => {
        api.getSession.mockRejectedValueOnce(new Error('load failed'))
        const { result } = renderHook(useWorkspaceData)
        await waitFor(() => expect(result.current.error).toBe('load failed'))
        act(() => result.current.selectSession('s1'))
        await waitFor(() => expect(result.current.agent?.id).toBe('a-s1'))
        expect(result.current.error).toBe('')
        expect(api.getSession).toHaveBeenCalledTimes(2)
    })

    it('删除 A 的完成回调不改变已切换到 B 的选择', async () => {
        api.listSessions.mockResolvedValue([summary('s1', '2026-01-03'), summary('s3', '2026-01-02'), summary('s2')])
        const pending = deferred<void>()
        api.deleteSession.mockReturnValue(pending.promise)
        const { result } = renderHook(() => {
            const workspace = useWorkspaceData()
            const [busy, setBusy] = useState(false)
            const commands = useSessionCommands({ ...workspace, busy, setBusy })
            return { ...workspace, ...commands }
        })
        await waitFor(() => expect(result.current.session?.id).toBe('s1'))
        let deletion!: Promise<boolean>
        act(() => { deletion = result.current.deleteSession('s1') })
        act(() => result.current.selectSession('s2'))
        await waitFor(() => expect(result.current.session?.id).toBe('s2'))
        api.listSessions.mockResolvedValue([summary('s3', '2026-01-02'), summary('s2')])
        await act(async () => { pending.resolve(); await deletion })
        expect(result.current.selectedSessionID).toBe('s2')
        expect(result.current.session?.id).toBe('s2')
    })

    it('任务结束定向更新标题和活动顺序，不重载模型目录', async () => {
        const { result } = renderHook(useWorkspaceData, { wrapper: StrictMode })
        await waitFor(() => expect(result.current.agent?.id).toBe('a-s1'))
        const modelQueries = api.listModels.mock.calls.length
        api.getSession.mockResolvedValue({ ...session('s1', '自动标题'), updatedAt: '2026-01-04' })
        act(() => api.listeners.forEach((listener) => listener({ kind: 'task_ended', agentId: 'a-s1', taskId: 't1', sessionId: 's1' })))
        await waitFor(() => expect(result.current.session?.title).toBe('自动标题'))
        expect(result.current.sessions[0].title).toBe('自动标题')
        expect(api.listModels).toHaveBeenCalledTimes(modelQueries)
    })

    it('切换会话后忽略旧 Agent 的延迟回填', async () => {
        api.listSessions.mockResolvedValue([summary('s1', '2026-01-03'), summary('s2')])
        const pending = deferred<ReturnType<typeof agent>>()
        api.getAgent.mockImplementation((id: string) => id === 'a-s1' ? pending.promise : Promise.resolve(agent(id, 's2')))
        const { result } = renderHook(useWorkspaceData)
        await waitFor(() => expect(api.getAgent).toHaveBeenCalledWith('a-s1'))
        act(() => result.current.selectSession('s2'))
        await waitFor(() => expect(result.current.agent?.id).toBe('a-s2'))
        await act(async () => pending.resolve(agent('a-s1', 's1')))
        expect(result.current.agent?.id).toBe('a-s2')
        expect(result.current.agents.map((item) => item.id)).toEqual(['a-s2'])
    })
})

it('旧查询不能覆盖查询期间开始的新任务', async () => {
    const pending = deferred<ReturnType<typeof agent>>()
    api.getAgent.mockReturnValue(pending.promise)
    const { result } = renderHook(useWorkspaceData)
    await waitFor(() => expect(api.getAgent).toHaveBeenCalledWith('a-s1'))
    act(() => api.listeners.forEach((listener) => listener({ kind: 'turn_started', agentId: 'a-s1', taskId: 'new-task', sessionId: 's1', turnId: 'new-turn' })))
    await act(async () => pending.resolve(agent('a-s1', 's1')))
    expect(result.current.agent?.currentTaskId).toBe('new-task')
    expect(result.current.agent?.state).toBe('executing')
})

it('迟到的自动标题回填不会覆盖用户重命名', async () => {
    const { result } = renderHook(useWorkspaceData)
    await waitFor(() => expect(result.current.agent?.id).toBe('a-s1'))
    const pending = deferred<SessionSnapshot>()
    api.getSession.mockReturnValue(pending.promise)
    act(() => api.listeners.forEach((listener) => listener({ kind: 'task_ended', agentId: 'a-s1', taskId: 't1', sessionId: 's1' })))
    act(() => result.current.updateSessionTitle('s1', '用户标题'))
    await act(async () => pending.resolve(session('s1', '旧自动标题')))
    expect(result.current.session?.title).toBe('用户标题')
    expect(result.current.sessions[0].title).toBe('用户标题')
})


it('刷新工作区取消旧分页后，仍能继续加载更早消息', async () => {
    const pending = deferred<AgentHistoryPage>()
    api.listAgentHistory.mockImplementation((_id: string, before = '') => before ? pending.promise : Promise.resolve({ items: [message(201)], nextCursor: 'message:201' }))
    const { result } = renderHook(useWorkspaceData)
    await waitFor(() => expect(result.current.historyCursor).toBe('message:201'))
    let loading!: Promise<void>
    act(() => { loading = result.current.loadOlderHistory() })
    expect(result.current.historyLoadingOlder).toBe(true)
    await act(async () => result.current.refresh())
    expect(result.current.historyLoadingOlder).toBe(false)
    await act(async () => { pending.resolve({ items: [message(200)], nextCursor: '' }); await loading })
    api.listAgentHistory.mockResolvedValue({ items: [message(199), message(200)], nextCursor: '' })
    await act(async () => result.current.loadOlderHistory())
    expect(result.current.history.map((item) => item.sequence)).toEqual([199, 200, 201])
    expect(result.current.historyCursor).toBe('')
})
