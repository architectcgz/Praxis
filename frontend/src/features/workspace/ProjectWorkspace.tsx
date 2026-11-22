import { useCallback, useEffect, useEffectEvent, useMemo, useState, type SubmitEvent } from 'react'
import { createPortal } from 'react-dom'
import { CircleAlert, FolderOpen, Globe, Layers, LoaderCircle, Settings2, X } from 'lucide-react'
import { Overlay } from '../../components/ui'
import { NewProjectDialog } from '../project/NewProjectDialog'
import { ProjectsPanel } from '../project/ProjectsPanel'
import { ProjectOverview } from '../project/ProjectOverview'
import { groupSessions } from '../project/projectGroups'
import type { ProjectGroup } from '../project/types'
import { SessionPanel } from '../session/SessionPanel'
import { SettingsPanel } from '../setting/SettingsPanel'
import { useProjectWorkspace } from './useProjectWorkspace'
import { SessionWorkspace, type SessionPageControls } from './SessionWorkspace'
import { WorkspaceHeader, type WorkspaceSessionTab } from './WorkspaceHeader'
import { WorkspaceRail, type WorkspacePage } from './WorkspaceRail'

export function ProjectWorkspace() {
    const workspace = useProjectWorkspace()
    const [lookupID, setLookupID] = useState('')
    const [searchOpen, setSearchOpen] = useState(false)
    const [projectOpen, setProjectOpen] = useState(false)
    const [navigationOpen, setNavigationOpen] = useState(() => window.innerWidth > 900)
    const [collapsedProjects, setCollapsedProjects] = useState<Record<string, boolean>>({})
    const [page, setPage] = useState<WorkspacePage>('session')
    const [selectedProjectID, setSelectedProjectID] = useState('')
    const [pageControls, setPageControls] = useState<Record<string, SessionPageControls>>({})
    const [closingSessionID, setClosingSessionID] = useState('')

    const activePage = pageControls[workspace.selectedSessionID]
    const projectGroups = useMemo(() => groupSessions(workspace.sessions, workspace.projects), [workspace.sessions, workspace.projects])
    const activeProject = page === 'project'
        ? projectGroups.find((item) => item.id === selectedProjectID)
        : projectGroups.find((item) => item.sessions.some((session) => session.id === workspace.selectedSessionID))

    const sessionProjectNameMap = useMemo(() => {
        const map = new Map<string, string>()
        for (const group of projectGroups) {
            for (const session of group.sessions) {
                map.set(session.id, group.name)
            }
        }
        return map
    }, [projectGroups])

    const sessionTabs: WorkspaceSessionTab[] = useMemo(() => workspace.openedSessionIDs.map((id) => {
        const controls = pageControls[id]
        const title = controls?.title || workspace.sessions.find((item) => item.id === id)?.title || '未命名会话'
        return {
            id,
            title,
            status: controls?.status || 'idle',
            queuedCount: controls?.queuedCount || 0,
            busy: Boolean(controls?.busy),
        }
    }), [workspace.openedSessionIDs, workspace.sessions, pageControls])

    const onPageState = useCallback((id: string, controls: SessionPageControls) => {
        setPageControls((current) => ({ ...current, [id]: controls }))
    }, [])

    const handleGlobalKeyDown = useEffectEvent((event: KeyboardEvent) => {
        if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'k') {
            event.preventDefault()
            setNavigationOpen((open) => !open)
        }
    })

    useEffect(() => {
        window.addEventListener('keydown', handleGlobalKeyDown)
        return () => window.removeEventListener('keydown', handleGlobalKeyDown)
    }, [])

    const collapseOnNarrow = () => {
        if (window.innerWidth <= 768) setNavigationOpen(false)
    }

    const selectSession = (id: string) => {
        collapseOnNarrow()
        setPage('session')
        workspace.selectSession(id)
    }
    const openWorkbench = () => {
        setPage('session')
    }
    const openWorld = () => {
        setPage('world')
    }
    const openSettings = () => {
        setProjectOpen(false)
        setPage('settings')
    }
    const openSession = (project: ProjectGroup) => {
        collapseOnNarrow()
        workspace.clearError()
        setPage('session')
        setSelectedProjectID(project.id)
        void workspace.createNewSession(project.id)
    }
    const selectProject = (project: ProjectGroup) => {
        collapseOnNarrow()
        workspace.clearError()
        setPage('project')
        setSelectedProjectID(project.id)
        setCollapsedProjects((current) => current[project.id] ? { ...current, [project.id]: false } : current)
    }
    const loadSession = (event: SubmitEvent<HTMLFormElement>) => {
        event.preventDefault()
        if (lookupID.trim() && workspace.bridgeAvailable) {
            selectSession(lookupID.trim())
            setSearchOpen(false)
        }
    }
    const closePage = async () => {
        const id = closingSessionID
        const controls = pageControls[id]
        if (controls && !await controls.close()) {
            selectSession(id)
            setClosingSessionID('')
            return
        }
        workspace.clearSelectedSession(id)
        setPageControls((current) => {
            const next = { ...current }
            delete next[id]
            return next
        })
        setClosingSessionID('')
    }
    const deleteSession = async (id: string) => {
        const removed = await workspace.deleteSession(id)
        if (removed) {
            setPageControls((current) => {
                const next = { ...current }
                delete next[id]
                return next
            })
        }
        return removed
    }

    if (workspace.workspaceLoading) {
        return (
            <main className="workspace-loading" role="status" aria-busy="true">
                <LoaderCircle size={20} className="is-spinning" aria-hidden="true" />
                <span>正在加载 Praxis</span>
            </main>
        )
    }

    const hasTabs = workspace.openedSessionIDs.length > 0
    const selectedProject: ProjectGroup = projectGroups.find((item) => item.id === selectedProjectID)
        || { id: selectedProjectID, name: '项目', path: '', sessions: [] }

    return (
        <div className={`app-shell${navigationOpen ? ' is-sidebar-open' : ''}`}>
            <WorkspaceRail
                activePage={page}
                navigationOpen={navigationOpen}
                onToggleNavigation={() => setNavigationOpen((open) => !open)}
                onOpenWorkbench={openWorkbench}
                onOpenWorld={openWorld}
                onOpenSettings={openSettings}
            />

            {navigationOpen && (
                <div className="workspace-sidebar-pane">
                    <ProjectsPanel
                        projectGroups={projectGroups}
                        sessionsCount={workspace.sessions.length}
                        selectedSessionID={page === 'session' ? workspace.selectedSessionID : ''}
                        selectedProjectID={page === 'project' ? selectedProjectID : ''}
                        activeProjectID={page === 'session' ? activeProject?.id || '' : ''}
                        lookupID={lookupID}
                        searchOpen={searchOpen}
                        collapsedProjects={collapsedProjects}
                        busy={workspace.busy}
                        bridgeAvailable={workspace.bridgeAvailable}
                        onLookupChange={setLookupID}
                        onLoadSession={loadSession}
                        onToggleSearch={() => setSearchOpen((open) => !open)}
                        onOpenProject={() => { workspace.clearError(); setProjectOpen(true) }}
                        onOpenSession={openSession}
                        onDeleteSession={deleteSession}
                        onRenameSession={(id, title) => workspace.renameSession(id, title)}
                        onRevealSessionFile={workspace.revealSessionFile}
                        onSelectProject={selectProject}
                        onSelectSession={selectSession}
                        onToggleProject={(id) => setCollapsedProjects((current) => ({ ...current, [id]: !current[id] }))}
                        onCloseNavigation={() => setNavigationOpen(false)}
                    />
                </div>
            )}

            <div className="workspace-main">
                <WorkspaceHeader
                    activePage={page}
                    selectedSessionID={workspace.selectedSessionID}
                    tabs={sessionTabs}
                    projectName={activeProject?.name}
                    navigationOpen={navigationOpen}
                    busy={workspace.busy || Boolean(activePage?.busy)}
                    refreshing={workspace.refreshing || Boolean(activePage?.refreshing)}
                    subagentSummary={page === 'session' ? activePage?.subagentSummary : undefined}
                    onSelectSession={selectSession}
                    onRequestCloseSession={setClosingSessionID}
                    onToggleNavigation={() => setNavigationOpen((open) => !open)}
                    onOpenOverview={page === 'session' ? activePage?.openOverview : undefined}
                />

                <div className="workspace-stage-body">
                    {workspace.openedSessionIDs.map((id) => (
                        <SessionWorkspace
                            key={id}
                            sessionID={id}
                            title={workspace.sessions.find((item) => item.id === id)?.title}
                            projectName={sessionProjectNameMap.get(id)}
                            active={page === 'session' && workspace.selectedSessionID === id}
                            sessionsCount={workspace.sessions.length}
                            projectsCount={projectGroups.length}
                            modalOpen={projectOpen || Boolean(closingSessionID)}
                            onOpenProject={() => { workspace.clearError(); setProjectOpen(true) }}
                            createNewSession={workspace.createNewSession}
                            refreshCatalog={workspace.refreshCatalog}
                            onOpenSettings={openSettings}
                            onStateChange={onPageState}
                        />
                    ))}

                    {(page !== 'session' || !hasTabs) && (
                        <div className="workbench-sheets">
                            <main className="main-panel workbench-sheet">
                                {page === 'settings' ? (
                                    <>
                                        <div className="workbench-sheet-head">
                                            <div className="workbench-sheet-head-left">
                                                <Settings2 size={15} strokeWidth={1.85} aria-hidden="true" />
                                                <strong className="workbench-sheet-title" id="settings-title">配置概览</strong>
                                                <span className="workbench-sheet-subtitle">本地配置文件是模型目录、凭据、工具启停和审批默认值的唯一来源</span>
                                            </div>
                                            <div className="workbench-sheet-head-right">
                                                <span className={`settings-status-badge ${activePage ? '' : 'is-muted'}`}>
                                                    {activePage ? `当前会话：${activePage.title || '未命名会话'}` : '尚未打开会话'}
                                                </span>
                                            </div>
                                        </div>
                                        <SettingsPanel
                                            models={activePage?.models || []}
                                            hasActiveSession={Boolean(activePage)}
                                        />
                                    </>
                                ) : page === 'project' ? (
                                    <>
                                        <div className="workbench-sheet-head">
                                            <div className="workbench-sheet-head-left">
                                                <FolderOpen size={15} strokeWidth={1.85} aria-hidden="true" />
                                                <strong className="workbench-sheet-title">{selectedProject.name}</strong>
                                                <span className="workbench-sheet-subtitle">项目概览</span>
                                            </div>
                                            <div className="workbench-sheet-head-right">
                                                <span className="workbench-sheet-meta">{selectedProject.sessions.length} 个会话</span>
                                            </div>
                                        </div>
                                        <ProjectOverview
                                            project={selectedProject}
                                            selectedSessionID={workspace.selectedSessionID}
                                            openedSessionIDs={workspace.openedSessionIDs}
                                            onSelectSession={selectSession}
                                            onOpenSession={openSession}
                                        />
                                    </>
                                ) : page === 'world' ? (
                                    <>
                                        <div className="workbench-sheet-head">
                                            <div className="workbench-sheet-head-left">
                                                <Globe size={15} strokeWidth={1.85} aria-hidden="true" />
                                                <strong className="workbench-sheet-title">共生推演世界</strong>
                                                <span className="workbench-sheet-subtitle">独立多 Agent 演化情景</span>
                                            </div>
                                            <div className="workbench-sheet-head-right">
                                                <span className="workbench-sheet-meta">独立页面</span>
                                            </div>
                                        </div>
                                        <section className="world-stage-placeholder" aria-label="独立世界推演页">
                                            <div className="world-stage-placeholder-icon" aria-hidden="true">
                                                <Globe size={24} strokeWidth={1.7} />
                                            </div>
                                            <h2>共生推演世界</h2>
                                            <p>独立于工作台的多 Agent 共生世界页面，用于呈现多个上下文隔离的代理在共享环境中持续交互并推动世界状态演化。</p>
                                            <button className="session-empty-cta" type="button" onClick={openWorkbench}>
                                                <Layers size={15} strokeWidth={1.9} aria-hidden="true" />
                                                <span>返回工作台</span>
                                            </button>
                                        </section>
                                    </>
                                ) : (
                                    <>
                                        <div className="workbench-sheet-head">
                                            <div className="workbench-sheet-head-left">
                                                <Layers size={15} strokeWidth={1.85} aria-hidden="true" />
                                                <strong className="workbench-sheet-title">工作台</strong>
                                                <span className="workbench-sheet-subtitle">多 Agent 协同工作区</span>
                                            </div>
                                            <div className="workbench-sheet-head-right">
                                                <span className="workbench-sheet-meta">
                                                    {projectGroups.length} 个项目 · {workspace.sessions.length} 个会话
                                                </span>
                                            </div>
                                        </div>
                                        <SessionPanel
                                            loading={false}
                                            hasSession={false}
                                            sessionsCount={workspace.sessions.length}
                                            projectsCount={projectGroups.length}
                                            onOpenProject={() => { workspace.clearError(); setProjectOpen(true) }}
                                        />
                                    </>
                                )}
                            </main>
                        </div>
                    )}
                </div>
            </div>

            {workspace.error && createPortal(
                <div className="error-banner" role="alert" aria-atomic="true">
                    <span className="error-banner-message">
                        <CircleAlert size={18} aria-hidden="true" />
                        <span>{workspace.error}</span>
                    </span>
                    <button
                        className="icon-button error-banner-close"
                        type="button"
                        title="关闭提示"
                        aria-label="关闭错误提示"
                        onClick={workspace.clearError}
                    >
                        <X size={15} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>,
                document.body,
            )}

            {projectOpen && (
                <NewProjectDialog
                    bridgeAvailable={workspace.bridgeAvailable}
                    onCreated={(id) => { void workspace.refreshCatalog(true); setSelectedProjectID(id); setPage('project') }}
                    onClose={() => setProjectOpen(false)}
                />
            )}

            {closingSessionID && (
                <Overlay
                    labelledBy="close-session-page-title"
                    onClose={() => { if (!pageControls[closingSessionID]?.busy) setClosingSessionID('') }}
                >
                    <section className="session-close-dialog">
                        <h2 id="close-session-page-title">关闭会话页？</h2>
                        <p>将停止此会话的全部执行并清空待办队列，未发送的草稿会丢失。已保存的会话历史仍然保留。</p>
                        <div className="session-close-actions">
                            <button
                                type="button"
                                className="modal-secondary"
                                disabled={pageControls[closingSessionID]?.busy}
                                onClick={() => setClosingSessionID('')}
                            >
                                取消
                            </button>
                            <button
                                type="button"
                                className="modal-primary modal-danger"
                                disabled={pageControls[closingSessionID]?.busy}
                                onClick={() => void closePage()}
                            >
                                结束执行并关闭
                            </button>
                        </div>
                    </section>
                </Overlay>
            )}
        </div>
    )
}
