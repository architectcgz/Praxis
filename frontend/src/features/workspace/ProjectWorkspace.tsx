import { useCallback, useEffect, useMemo, useRef, useState, type SubmitEvent } from 'react'
import { createPortal } from 'react-dom'
import { CircleAlert, LoaderCircle, X } from 'lucide-react'
import { Overlay } from '../../components/ui'
import { AgentsPanel } from '../agent/AgentsPanel'
import { NewProjectDialog } from '../project/NewProjectDialog'
import { ProjectsPanel } from '../project/ProjectsPanel'
import { ProjectOverview } from '../project/ProjectOverview'
import { groupSessions } from '../project/projectGroups'
import type { ProjectGroup } from '../project/types'
import { SessionPanel } from '../session/SessionPanel'
import { SettingsPanel } from '../setting/SettingsPanel'
import { useProjectWorkspace } from './useProjectWorkspace'
import { SessionWorkspace, type SessionPageControls } from './SessionWorkspace'
import { WorkspaceHeader } from './WorkspaceHeader'


export function ProjectWorkspace() {
    const workspace = useProjectWorkspace()
    const [lookupID, setLookupID] = useState('')
    const [searchOpen, setSearchOpen] = useState(false)
    const [projectOpen, setProjectOpen] = useState(false)
    const [navigationOpen, setNavigationOpen] = useState(false)
    const navigationWasOpen = useRef(false)
    const [collapsedProjects, setCollapsedProjects] = useState<Record<string, boolean>>({})
    const [page, setPage] = useState<'project' | 'session' | 'settings'>('session')
    const [selectedProjectID, setSelectedProjectID] = useState('')
    const [pageControls, setPageControls] = useState<Record<string, SessionPageControls>>({})
    const [closingSessionID, setClosingSessionID] = useState('')
    const settingsOpen = page === 'settings'
    const activePage = pageControls[workspace.selectedSessionID]
    const projectGroups = useMemo(() => groupSessions(workspace.sessions, workspace.projects), [workspace.sessions, workspace.projects])
    const activeProject = page === 'project' ? projectGroups.find((item) => item.id === selectedProjectID)
        : projectGroups.find((item) => item.sessions.some((session) => session.id === workspace.selectedSessionID))
    const onPageState = useCallback((id: string, controls: SessionPageControls) => {
        setPageControls((current) => ({ ...current, [id]: controls }))
    }, [])
    useEffect(() => {
        const media = window.matchMedia('(max-width: 860px)')
        if (!navigationOpen) {
            if (navigationWasOpen.current && !media.matches) document.querySelector<HTMLButtonElement>('.projects-panel button')?.focus({ preventScroll: true })
            navigationWasOpen.current = false
            return
        }
        navigationWasOpen.current = true
        const host = document.getElementById('overlay-root')
        const closeOnWideScreen = () => { if (!media.matches && (host?.childElementCount ?? 0) <= 1) setNavigationOpen(false) }
        const observer = new MutationObserver(closeOnWideScreen)
        if (host) observer.observe(host, { childList: true })
        media.addEventListener('change', closeOnWideScreen)
        closeOnWideScreen()
        return () => { observer.disconnect(); media.removeEventListener('change', closeOnWideScreen) }
    }, [navigationOpen])

    const selectSession = (id: string) => {
        setNavigationOpen(false)
        setPage('session')
        workspace.selectSession(id)
    }
    const openSettings = () => { setNavigationOpen(false); setProjectOpen(false); setPage('settings') }
    const openSession = (project: ProjectGroup) => {
        setNavigationOpen(false); workspace.clearError(); setPage('session'); setSelectedProjectID(project.id)
        void workspace.createNewSession(project.id)
    }
    const selectProject = (project: ProjectGroup) => {
        setNavigationOpen(false); workspace.clearError(); setPage('project'); setSelectedProjectID(project.id)
        setCollapsedProjects((current) => current[project.id] ? { ...current, [project.id]: false } : current)
    }
    const loadSession = (event: SubmitEvent<HTMLFormElement>) => {
        event.preventDefault()
        if (lookupID.trim() && workspace.bridgeAvailable) { selectSession(lookupID.trim()); setSearchOpen(false) }
    }
    const refresh = async () => { await Promise.all([workspace.refreshCatalog(true), activePage?.refresh()]) }
    const closePage = async () => {
        const id = closingSessionID
        const controls = pageControls[id]
        if (controls && !await controls.close()) { selectSession(id); setClosingSessionID(''); return }
        workspace.clearSelectedSession(id)
        setPageControls((current) => { const next = { ...current }; delete next[id]; return next })
        setClosingSessionID('')
    }
    const deleteSession = async (id: string) => {
        const removed = await workspace.deleteSession(id)
        if (removed) setPageControls((current) => { const next = { ...current }; delete next[id]; return next })
        return removed
    }
    if (workspace.workspaceLoading) return <main className="workspace-loading" role="status" aria-busy="true"><LoaderCircle size={20} className="is-spinning" aria-hidden="true" /><span>正在加载 Praxis</span></main>

    const projectsPanel = <ProjectsPanel
        projectGroups={projectGroups}
        sessionsCount={workspace.sessions.length}
        selectedSessionID={page === 'session' ? workspace.selectedSessionID : ''}
        selectedProjectID={page === 'project' ? selectedProjectID : ''}
        activeProjectID={page === 'session' ? activeProject?.id || '' : ''}
        lookupID={lookupID} searchOpen={searchOpen} collapsedProjects={collapsedProjects}
        busy={workspace.busy} bridgeAvailable={workspace.bridgeAvailable}
        onLookupChange={setLookupID} onLoadSession={loadSession} onToggleSearch={() => setSearchOpen((open) => !open)}
        onOpenProject={() => { setNavigationOpen(false); workspace.clearError(); setProjectOpen(true) }}
        onOpenSession={openSession} onDeleteSession={deleteSession}
        onRenameSession={(id, title) => workspace.renameSession(id, title)} onRevealSessionFile={workspace.revealSessionFile}
        onSelectProject={selectProject} onSelectSession={selectSession}
        onToggleProject={(id) => setCollapsedProjects((current) => ({ ...current, [id]: !current[id] }))}
        onOpenSettings={openSettings} settingsOpen={settingsOpen}
        onCloseNavigation={navigationOpen ? () => setNavigationOpen(false) : undefined}
    />
    const hasTabs = workspace.openedSessionIDs.length > 0
    const headerProjectName = settingsOpen ? '工作区设置' : activeProject?.name
    const headerSessionTitle = settingsOpen ? undefined
        : page === 'project' ? '项目概览'
        : hasTabs ? (activePage?.title || '未命名会话')
        : undefined
    return <div className="app-shell" inert={navigationOpen}>
        <WorkspaceHeader
            busy={workspace.busy || Boolean(activePage?.busy)} refreshing={workspace.refreshing || Boolean(activePage?.refreshing)}
            projectName={headerProjectName}
            sessionTitle={headerSessionTitle}
            onRefresh={() => void refresh()} navigationOpen={navigationOpen} onOpenNavigation={() => setNavigationOpen(true)}
        />
        <div className="workspace-grid">
            {navigationOpen ? <Overlay labelledBy="workspace-navigation-title" onClose={() => setNavigationOpen(false)} className="workspace-navigation-overlay">{projectsPanel}</Overlay> : projectsPanel}
            <div className={`workspace-stage${hasTabs ? ' has-session-tabs' : ''}`}>
                {hasTabs && <nav className="session-tabs" aria-label="已打开的会话">
                    {workspace.openedSessionIDs.map((id) => {
                        const controls = pageControls[id]
                        const title = controls?.title || workspace.sessions.find((item) => item.id === id)?.title || '未命名会话'
                        const tabStatus = controls?.status || 'idle'
                        const queuedCount = controls?.queuedCount || 0
                        const isActive = page === 'session' && workspace.selectedSessionID === id
                        return <div className="session-tab" key={id} data-active={isActive} data-status={tabStatus}>
                            <button type="button" className="session-tab-select" aria-current={isActive ? 'page' : undefined} onClick={() => selectSession(id)} title={`${title}${queuedCount > 0 ? ` · ${queuedCount} 个待办` : ''}`}>
                                {tabStatus !== 'idle' && <span className={`session-tab-dot is-${tabStatus}`} aria-hidden="true" />}
                                <span className="session-tab-label">{title}</span>
                                {queuedCount > 0 && <span className="session-tab-queue" title={`${queuedCount} 个待执行任务`}>{queuedCount}</span>}
                            </button>
                            <button type="button" className="icon-button session-tab-close" disabled={controls?.busy} onClick={() => setClosingSessionID(id)} aria-label={`关闭会话页：${title}`} title="关闭会话页"><X size={13} aria-hidden="true" /></button>
                        </div>
                    })}
                </nav>}
                <div className="workspace-stage-body">
                    {workspace.openedSessionIDs.map((id) => <SessionWorkspace
                        key={id} sessionID={id} title={workspace.sessions.find((item) => item.id === id)?.title}
                        active={page === 'session' && workspace.selectedSessionID === id}
                        sessionsCount={workspace.sessions.length} projectsCount={projectGroups.length}
                        modalOpen={navigationOpen || projectOpen || Boolean(closingSessionID)}
                        onOpenProject={() => { setNavigationOpen(false); workspace.clearError(); setProjectOpen(true) }}
                        createNewSession={workspace.createNewSession} refreshCatalog={workspace.refreshCatalog}
                        onOpenSettings={openSettings} onStateChange={onPageState}
                    />)}
                    {(page !== 'session' || !hasTabs) && <>
                        <main className="main-panel">
                            {page === 'settings' ? <SettingsPanel models={activePage?.models || []} hasActiveSession={Boolean(activePage)} activeSessionTitle={activePage?.title} />
                            : page === 'project' ? <ProjectOverview project={projectGroups.find((item) => item.id === selectedProjectID) || { id: selectedProjectID, name: '项目', path: '', sessions: [] }} selectedSessionID={workspace.selectedSessionID} openedSessionIDs={workspace.openedSessionIDs} onSelectSession={selectSession} onOpenSession={openSession} />
                            : <SessionPanel loading={false} hasSession={false} sessionsCount={workspace.sessions.length} projectsCount={projectGroups.length} onOpenProject={() => { setNavigationOpen(false); workspace.clearError(); setProjectOpen(true) }} />}
                        </main>
                        <AgentsPanel loading={false} session={null} agentEntries={{}} selectedAgentID="" onSelectAgent={() => { }} />
                    </>}
                </div>
            </div>
        </div>
        {workspace.error && createPortal(
            <div className={`error-banner${hasTabs ? ' has-tabs' : ''}`} role="alert" aria-atomic="true">
                <span className="error-banner-message"><CircleAlert size={18} aria-hidden="true" /><span>{workspace.error}</span></span>
                <button className="icon-button error-banner-close" type="button" title="关闭提示" aria-label="关闭错误提示" onClick={workspace.clearError}>
                    <X size={15} strokeWidth={1.8} aria-hidden="true" />
                </button>
            </div>,
            document.body,
        )}
        {projectOpen && <NewProjectDialog bridgeAvailable={workspace.bridgeAvailable} onCreated={(id) => { void workspace.refreshCatalog(true); setSelectedProjectID(id); setPage('project') }} onClose={() => setProjectOpen(false)} />}
        {closingSessionID && <Overlay labelledBy="close-session-page-title" onClose={() => { if (!pageControls[closingSessionID]?.busy) setClosingSessionID('') }}>
            <section className="session-close-dialog">
                <h2 id="close-session-page-title">关闭会话页？</h2>
                <p>将停止此会话的全部执行并清空待办队列，未发送的草稿会丢失。已保存的会话历史仍然保留。</p>
                <div className="session-close-actions">
                    <button type="button" className="modal-secondary" disabled={pageControls[closingSessionID]?.busy} onClick={() => setClosingSessionID('')}>取消</button>
                    <button type="button" className="modal-primary modal-danger" disabled={pageControls[closingSessionID]?.busy} onClick={() => void closePage()}>结束执行并关闭</button>
                </div>
            </section>
        </Overlay>}
    </div>
}
