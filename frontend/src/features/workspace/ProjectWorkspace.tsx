import { lazy, Suspense, useEffect, useMemo, useRef, useState, type SubmitEvent } from 'react'
import { LoaderCircle } from 'lucide-react'
import { Overlay, Toast } from '../../components/ui'
import { AgentSessionPanel } from '../agents/AgentSessionPanel'
import { AgentsPanel } from '../agents/AgentsPanel'
import { NewProjectDialog } from '../projects/NewProjectDialog'
import { ProjectsPanel } from '../projects/ProjectsPanel'
import { groupSessions } from '../projects/projectGroups'
import { SessionPanel } from '../sessions/SessionPanel'
import { SettingsPanel } from '../settings/SettingsPanel'
import { useProjectWorkspace } from './useProjectWorkspace'
import { WORKSPACE_COMMANDS } from './useWorkspaceCommands'
import { WorkspaceHeader } from './WorkspaceHeader'
import { API_ERROR_CODES } from '../../api'
import { ProjectGroup } from '../projects/types'
import { ProjectOverview } from '../projects/ProjectOverview'

const ModelConfigPanel = lazy(() => import('../settings/model-config/ModelConfigPanel').then((module) => ({ default: module.ModelConfigPanel })))

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
    const settingsOpen = page === 'settings' || page === 'models'
    const projectGroups = useMemo(() => groupSessions(workspace.sessions, workspace.projects), [workspace.sessions, workspace.projects])
    const activeProject = projectGroups.find((item) => item.id === selectedProjectID || item.sessions.some((session) => session.id === workspace.selectedSessionID))

    useEffect(() => {
        setLookupID(workspace.selectedSessionID)
    }, [workspace.selectedSessionID])

    useEffect(() => {
        const media = window.matchMedia('(max-width: 860px)')
        if (!navigationOpen) {
            if (navigationWasOpen.current && !media.matches) {
                document.querySelector<HTMLButtonElement>('.projects-panel button')?.focus({ preventScroll: true })
            }
            navigationWasOpen.current = false
            return
        }
        navigationWasOpen.current = true
        const host = document.getElementById('overlay-root')
        const closeOnWideScreen = () => {
            // 编辑或确认弹窗关闭后再恢复侧栏，窗口尺寸变化不丢弃草稿。
            if (!media.matches && (host?.childElementCount ?? 0) <= 1) setNavigationOpen(false)
        }
        const observer = new MutationObserver(closeOnWideScreen)
        if (host) observer.observe(host, { childList: true })
        media.addEventListener('change', closeOnWideScreen)
        closeOnWideScreen()
        return () => {
            observer.disconnect()
            media.removeEventListener('change', closeOnWideScreen)
        }
    }, [navigationOpen])

    const selectSession = (id: string) => {
        setNavigationOpen(false)
        setLookupID(id)
        setPage('session')
        const project = projectGroups.find((item) => item.sessions.some((session) => session.id === id))
        if (project) {
            setSelectedProjectID(project.id)
        }
        if (id === workspace.selectedSessionID && workspace.session) {
            return
        }
        workspace.selectSession(id)
    }

    const loadSession = (event: SubmitEvent<HTMLFormElement>) => {
        event.preventDefault()
        const nextID = lookupID.trim()
        if (nextID && workspace.bridgeAvailable) {
            selectSession(nextID)
            setSearchOpen(false)
        }
    }

    const openProject = () => {
        setNavigationOpen(false)
        workspace.clearError()
        setPage('session')
        setProjectOpen(true)
    }

    const openSession = (project: ProjectGroup) => {
        setNavigationOpen(false)
        workspace.clearError()
        setPage('session')
        setSelectedProjectID(project.id)
        void workspace.createNewSession(project.id, project.workspaceID)
    }

    const selectProject = (project: ProjectGroup) => {
        setNavigationOpen(false)
        workspace.clearError()
        setPage('project')
        setSelectedProjectID(project.id)
        setCollapsedProjects((current) => current[project.id] === true ? { ...current, [project.id]: false } : current)
    }

    const openSettings = () => {
        setNavigationOpen(false)
        setProjectOpen(false)
        if (workspace.errorCode !== API_ERROR_CODES.modelNotConfigured) {
            workspace.clearError()
        }
        setPage('settings')
    }

    const projectsPanel = (
        <ProjectsPanel
            projectGroups={projectGroups}
            sessionsCount={workspace.sessions.length}
            selectedSessionID={workspace.selectedSessionID}
            selectedProjectID={selectedProjectID}
            lookupID={lookupID}
            searchOpen={searchOpen}
            collapsedProjects={collapsedProjects}
            busy={workspace.busy}
            bridgeAvailable={workspace.bridgeAvailable}
            onLookupChange={setLookupID}
            onLoadSession={loadSession}
            onToggleSearch={() => setSearchOpen((open) => !open)}
            onOpenProject={openProject}
            onOpenSession={openSession}
            onDeleteSession={(id) => workspace.deleteSession(id)}
            onRenameSession={(id, title) => workspace.renameSession(id, title)}
            onSelectProject={selectProject}
            onSelectSession={selectSession}
            onToggleProject={(id) => setCollapsedProjects((current) => ({
                ...current,
                [id]: !current[id],
            }))}
            onOpenSettings={openSettings}
            settingsOpen={settingsOpen}
            onCloseNavigation={navigationOpen ? () => setNavigationOpen(false) : undefined}
        />
    )

    return (
        <div className="app-shell" inert={navigationOpen}>
            <WorkspaceHeader
                busy={workspace.busy}
                refreshing={workspace.refreshing}
                projectName={page === 'settings' || page === 'models' ? '设置' : activeProject?.name}
                sessionTitle={page === 'models' ? 'Provider 与 Model' : page === 'project' ? '项目概览' : workspace.session?.title}
                onRefresh={() => void workspace.refresh(true)}
                navigationOpen={navigationOpen}
                onOpenNavigation={() => setNavigationOpen(true)}
            />

            <div className="workspace-grid">
                {navigationOpen ? (
                    <Overlay labelledBy="workspace-navigation-title" onClose={() => setNavigationOpen(false)} className="workspace-navigation-overlay">
                        {projectsPanel}
                    </Overlay>
                ) : projectsPanel}

                <main className="main-panel">
                    {page === 'settings' ? (
                        <SettingsPanel
                            models={workspace.models}
                            onOpenModels={() => setPage('models')}
                        />
                    ) : page === 'models' ? (
                        <Suspense fallback={
                            <div className="management-loading" role="status" aria-label="加载配置中">
                                <LoaderCircle size={20} className="is-spinning" aria-hidden="true" />
                            </div>
                        }>
                            <ModelConfigPanel
                                onRefresh={() => void workspace.refresh(true)}
                                onBack={() => setPage('settings')}
                            />
                        </Suspense>
                    ) : page === 'project' ? (
                        <ProjectOverview
                            project={projectGroups.find((item) => item.id === selectedProjectID) || { id: selectedProjectID, workspaceID: '', name: '项目', path: '', sessions: [] }}
                            selectedSessionID={workspace.selectedSessionID}
                            onSelectSession={selectSession}
                            onOpenSession={openSession}
                        />
                    ) : (
                        <>
                            {workspace.error && (
                                <div className="error-banner" role="alert">
                                    <span>{workspace.error}</span>
                                    {workspace.errorCode === API_ERROR_CODES.modelNotConfigured && (
                                        <button className="error-banner-action" type="button" onClick={openSettings}>打开设置</button>
                                    )}
                                </div>
                            )}
                            <SessionPanel
                                key={`${workspace.selectedSessionID || 'session-empty'}-${workspace.session ? 'loaded' : 'loading'}`}
                                loading={Boolean(workspace.selectedSessionID && !workspace.session)}
                                hasSession={Boolean(workspace.session)}
                                sessionsCount={workspace.sessions.length}
                            >
                                {workspace.session && (
                                    <AgentSessionPanel
                                        loading={workspace.agentLoading}
                                        agents={workspace.agents}
                                        agent={workspace.agent}
                                        history={workspace.history}
                                        agentHistories={workspace.agentHistories}
                                        agentHistoryErrors={workspace.agentHistoryErrors}
                                        streamingOutputs={workspace.streamingOutputs}
                                        onSelectAgent={(id) => void workspace.selectAgent(id)}
                                        streamingOutput={workspace.streamingOutput}
                                        pendingUserMessages={workspace.pendingUserMessages}
                                        awaitingOutput={workspace.awaitingOutput}
                                        input={workspace.input}
                                        setInput={workspace.setInput}
                                        models={workspace.models}
                                        selectedProviderID={workspace.selectedProviderID}
                                        selectedModelID={workspace.selectedModelID}
                                        reasoning={workspace.reasoning}
                                        busy={workspace.busy}
                                        onSend={() => void workspace.submitInput()}
                                        onControl={(kind) => void workspace.control(kind)}
                                        onModelChange={workspace.selectModel}
                                        onReasoningChange={workspace.selectReasoning}
                                        commands={WORKSPACE_COMMANDS}
                                        onCommand={workspace.runCommand}
                                    />
                                )}
                            </SessionPanel>
                        </>
                    )}
                </main>

                <AgentsPanel
                    loading={page === 'session' && Boolean(workspace.selectedSessionID && !workspace.session)}
                    session={page === 'session' && workspace.session ? {
                        title: workspace.session.title,
                        agents: workspace.agents,
                    } : null}
                    histories={workspace.agentHistories}
                    selectedAgentID={workspace.selectedAgentID}
                    onSelectAgent={(id) => void workspace.selectAgent(id)}
                />
            </div>

            <Toast message={workspace.error ? '' : workspace.commandNotice} onDismiss={workspace.clearCommandNotice} />

            {projectOpen && (
                <NewProjectDialog
                    bridgeAvailable={workspace.bridgeAvailable}
                    onCreated={(projectID) => {
                        void workspace.refreshCatalog(true)
                        setSelectedProjectID(projectID)
                        setPage('project')
                    }}
                    onClose={() => {
                        setProjectOpen(false)
                        document.querySelector<HTMLButtonElement>('.workspace-navigation-trigger')?.focus({ preventScroll: true })
                    }}
                />
            )}
        </div>
    )
}
