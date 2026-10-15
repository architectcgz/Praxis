import { fireEvent, render, screen } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { session, summary } from '../../test/fixtures'
import { ProjectWorkspace } from './ProjectWorkspace'
import { useProjectWorkspace } from './useProjectWorkspace'

vi.mock('./useProjectWorkspace')
vi.mock('../agents/AgentSessionPanel', () => ({ AgentSessionPanel: () => null }))

it('打开其他项目时，顶部名称和侧栏选择取自当前页面', () => {
    vi.mocked(useProjectWorkspace).mockReturnValue({
        selectedSessionID: 's1', session: session(), sessions: [summary()],
        projects: [
            { id: 'p1', name: 'Old', defaultWorkspaceId: 'w1', path: '/old', state: 'ready' },
            { id: 'p2', name: 'New', defaultWorkspaceId: 'w2', path: '/new', state: 'ready' },
        ],
        agents: [], agentHistories: {}, bridgeAvailable: true, workspaceLoading: false, sessionLoading: false,
        busy: false, refreshing: false, error: '', commandNotice: '', clearError: vi.fn(),
    } as unknown as ReturnType<typeof useProjectWorkspace>)
    const { container } = render(<ProjectWorkspace />)
    fireEvent.click(screen.getByRole('button', { name: '打开项目 New' }))
    expect(container.querySelector('.crumb')?.textContent).toBe('New / 项目概览')
    expect(container.querySelectorAll('.project-heading.selected')).toHaveLength(1)
    expect(container.querySelector('.project-heading.selected')?.textContent).toContain('New')
})
