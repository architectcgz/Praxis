import { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState, type SubmitEvent } from 'react'
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

const ModelConfigPanel = lazy(() => import('../setting/model-config/ModelConfigPanel').then((module) => ({ default: module.ModelConfigPanel })))

export function ProjectWorkspace() {
    const workspace = useProjectWorkspace()
    const [lookupID, setLookupID] = useState(workspace.selectedSessionID)
    const [searchOpen, setSearchOpen] = useState(false)
    const [projectOpen, setProjectOpen] = useState(false)
    const [navigationOpen, setNavigationOpen] = useState(false)
    const navigationWasOpen = useRef(false)
    const [collapsedProjects, setCollapsedProjects] = useState<Record<string, boolean>>({})
    const [page, setPage] = useState<'project' | 'session' | 'settings' | 'models'>('session')
    const [selectedProjectID, setSelectedProjectID] = useState('')
    const [pageControls, setPageControls] = useState<Record<string, SessionPageControls>>({})
    const [closingSessionID, setClosingSessionID] = useState('')
    const settingsOpen = page === 'settings' || page === 'models'
    const activePage = pageControls[workspace.selectedSessionID]
    const projectGroups = useMemo(() => groupSessions(workspace.sessions, workspace.projects), [workspace.sessions, workspace.projects])
    const activeProject = page === 'project' ? projectGroups.find((item) => item.id === selectedProjectID)
        : projectGroups.find((item) => item.sessions.some((session) => session.id === workspace.selectedSessionID))
    const onPageState = useCallback((id: string, controls: SessionPageControls) => {
        setPageControls((current) => ({ ...current, [id]: controls }))
    }, [])
    useEffect(() => { setLookupID(workspace.selectedSessionID) }, [workspace.selectedSessionID])
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
        setLookupID(id)
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
        selectedProjectID={page === 'project' ? selectedProjectID : page === 'session' ? activeProject?.id || '' : ''}
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
    return <div className={`app-shell${workspace.openedSessionIDs.length ? ' has-session-tabs' : ''}`} inert={navigationOpen}>
        <WorkspaceHeader
            busy={workspace.busy || Boolean(activePage?.busy)} refreshing={workspace.refreshing || Boolean(activePage?.refreshing)}
            projectName={settingsOpen ? '设置' : activeProject?.name}
            sessionTitle={page === 'models' ? 'Provider 与 Model' : page === 'project' ? '项目概览' : activePage?.title}
            onRefresh={() => void refresh()} navigationOpen={navigationOpen} onOpenNavigation={() => setNavigationOpen(true)}
        />
        {workspace.openedSessionIDs.length > 0 && <nav className="session-tabs" aria-label="已打开的会话">
            {workspace.openedSessionIDs.map((id) => {
                const title = pageControls[id]?.title || workspace.sessions.find((item) => item.id === id)?.title || '未命名会话'
                return <div className="session-tab" key={id} data-active={page === 'session' && workspace.selectedSessionID === id}>
                    <button type="button" className="session-tab-select" aria-current={page === 'session' && workspace.selectedSessionID === id ? 'page' : undefined} onClick={() => selectSession(id)} title={title}>{title}</button>
                    <button type="button" className="icon-button session-tab-close" disabled={pageControls[id]?.busy} onClick={() => setClosingSessionID(id)} aria-label={`关闭会话页：${title}`} title="关闭会话页"><X size={13} aria-hidden="true" /></button>
                </div>
            })}
        </nav>}
        <div className="workspace-grid">
            {navigationOpen ? <Overlay labelledBy="workspace-navigation-title" onClose={() => setNavigationOpen(false)} className="workspace-navigation-overlay">{projectsPanel}</Overlay> : projectsPanel}
            {workspace.openedSessionIDs.map((id) => <SessionWorkspace
                key={id} sessionID={id} title={workspace.sessions.find((item) => item.id === id)?.title}
                active={page === 'session' && workspace.selectedSessionID === id}
                sessionsCount={workspace.sessions.length} modalOpen={navigationOpen || projectOpen || Boolean(closingSessionID)}
                createNewSession={workspace.createNewSession} refreshCatalog={workspace.refreshCatalog}
                onOpenSettings={openSettings} onStateChange={onPageState}
            />)}
            {(page !== 'session' || !workspace.openedSessionIDs.length) && <>
                <main className="main-panel">
                    {page === 'settings' ? <SettingsPanel models={activePage?.models || []} onOpenModels={() => setPage('models')} />
                    : page === 'models' ? <Suspense fallback={<div className="management-loading" role="status" aria-label="加载配置中"><LoaderCircle size={20} className="is-spinning" aria-hidden="true" /></div>}><ModelConfigPanel onRefresh={() => void refresh()} onBack={() => setPage('settings')} /></Suspense>
                    : page === 'project' ? <ProjectOverview project={projectGroups.find((item) => item.id === selectedProjectID) || { id: selectedProjectID, name: '项目', path: '', sessions: [] }} selectedSessionID={workspace.selectedSessionID} onSelectSession={selectSession} onOpenSession={openSession} />
                    : <SessionPanel loading={false} hasSession={false} sessionsCount={workspace.sessions.length} />}
                </main>
                <AgentsPanel loading={false} session={null} agentEntries={{}} selectedAgentID="" onSelectAgent={() => { }} />
            </>}
        </div>
        {workspace.error && createPortal(<div className="error-banner" role="alert" aria-atomic="true"><span className="error-banner-message"><CircleAlert size={18} aria-hidden="true" /><span>{workspace.error}</span></span></div>, document.body)}
        {projectOpen && <NewProjectDialog bridgeAvailable={workspace.bridgeAvailable} onCreated={(id) => { void workspace.refreshCatalog(true); setSelectedProjectID(id); setPage('project') }} onClose={() => setProjectOpen(false)} />}
        {closingSessionID && <Overlay labelledBy="close-session-page-title" onClose={() => { if (!pageControls[closingSessionID]?.busy) setClosingSessionID('') }}>
            <section className="session-close-dialog">
                <h2 id="close-session-page-title">关闭会话页？</h2>
                <p>将停止此会话的全部执行并清空待办队列，未发送的草稿会丢失。已保存的会话历史仍然保留，重新打开时需重新选择模型。</p>
                <div className="session-close-actions">
                    <button type="button" className="modal-secondary" disabled={pageControls[closingSessionID]?.busy} onClick={() => setClosingSessionID('')}>取消</button>
                    <button type="button" className="modal-primary modal-danger" disabled={pageControls[closingSessionID]?.busy} onClick={() => void closePage()}>结束执行并关闭</button>
                </div>
            </section>
        </Overlay>}
    </div>
}
