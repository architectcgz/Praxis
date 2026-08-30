import { FormEvent, useEffect, useMemo, useState } from 'react'
import { AgentsPanel } from '../agents/AgentsPanel'
import { NewProjectDialog } from '../projects/NewProjectDialog'
import { NewSessionDialog } from '../sessions/NewSessionDialog'
import { ProjectsPanel } from '../projects/ProjectsPanel'
import { groupSessions } from '../projects/projectGroups'
import { SessionPanel } from '../sessions/SessionPanel'
import { SettingsPanel } from '../settings/SettingsPanel'
import { ModelConfigPanel } from '../settings/ModelConfigPanel'
import { useProjectWorkspace } from './useProjectWorkspace'
import { WorkspaceHeader } from './WorkspaceHeader'
import { API_ERROR_CODES } from '../../api'
import { ProjectGroup } from '../projects/types'
import { ProjectOverview } from '../projects/ProjectOverview'

export function ProjectWorkspace() {
    const workspace = useProjectWorkspace()
    const [lookupID, setLookupID] = useState(workspace.selectedSessionID)
    const [searchOpen, setSearchOpen] = useState(false)
    const [projectOpen, setProjectOpen] = useState(false)
    const [sessionProject, setSessionProject] = useState<ProjectGroup | null>(null)
    const [collapsedProjects, setCollapsedProjects] = useState<Record<string, boolean>>({})
    const [page, setPage] = useState<'project' | 'session' | 'settings' | 'models'>('session')
    const [selectedProjectID, setSelectedProjectID] = useState('')
    const settingsOpen = page === 'settings' || page === 'models'
    const projectGroups = useMemo(() => groupSessions(workspace.sessions, workspace.projects), [workspace.sessions, workspace.projects])

    useEffect(() => {
        setLookupID(workspace.selectedSessionID)
    }, [workspace.selectedSessionID])

    const selectSession = (id: string) => {
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

    const loadSession = (event: FormEvent<HTMLFormElement>) => {
        event.preventDefault()
        const nextID = lookupID.trim()
        if (nextID && workspace.bridgeAvailable) {
            selectSession(nextID)
            setSearchOpen(false)
        }
    }

    const openProject = () => {
        workspace.clearError()
        setPage('session')
        setProjectOpen(true)
    }

    const openSession = (project: ProjectGroup) => {
        workspace.clearError()
        setPage('session')
        setSessionProject(project)
    }

    const selectProject = (project: ProjectGroup) => {
        workspace.clearError()
        setPage('project')
        setSelectedProjectID(project.id)
        setCollapsedProjects((current) => current[project.id] === true ? { ...current, [project.id]: false } : current)
    }

    const openSettings = () => {
        setProjectOpen(false)
        setSessionProject(null)
        if (workspace.errorCode !== API_ERROR_CODES.modelNotConfigured) {
            workspace.clearError()
        }
        setPage('settings')
    }

    return (
        <div className="app-shell">
            <WorkspaceHeader
                bridgeState={workspace.bridgeState}
                ready={workspace.health.ready}
                issue={Boolean(workspace.health.issue)}
                busy={workspace.busy}
                refreshing={workspace.refreshing}
                onRefresh={() => void workspace.refresh(true)}
            />

            <div className="workspace-grid">
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
                    healthReady={workspace.health.ready}
                    onLookupChange={setLookupID}
                    onLoadSession={loadSession}
                    onToggleSearch={() => setSearchOpen((open) => !open)}
                    onOpenProject={openProject}
                    onOpenSession={openSession}
                    onSelectProject={selectProject}
                    onSelectSession={selectSession}
                    onToggleProject={(id) => setCollapsedProjects((current) => ({
                        ...current,
                        [id]: !current[id],
                    }))}
                    onOpenSettings={openSettings}
                    settingsOpen={settingsOpen}
                />

                <main className="main-panel">
                    {page === 'settings' ? (
                        <SettingsPanel
                            models={workspace.models}
                            refreshing={workspace.refreshing}
                            onRefresh={() => void workspace.refresh(true)}
                            onOpenModels={() => setPage('models')}
                        />
                    ) : page === 'models' ? (
                        <ModelConfigPanel
                            onRefresh={() => void workspace.refresh(true)}
                            onBack={() => setPage('settings')}
                        />
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
                                loading={Boolean(workspace.selectedSessionID && !workspace.session) || workspace.agentLoading}
                                session={workspace.session}
                                sessionsCount={workspace.sessions.length}
                                agent={workspace.agent}
                                history={workspace.history}
                                streamingOutput={workspace.streamingOutput}
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
                            />
                        </>
                    )}
                </main>

                <AgentsPanel
                    loading={page === 'session' && Boolean(workspace.selectedSessionID && !workspace.session)}
                    session={page === 'session' ? workspace.session : null}
                    selectedAgentID={workspace.selectedAgentID}
                    onSelectAgent={(id) => void workspace.selectAgent(id)}
                />
            </div>

            {projectOpen && (
                <NewProjectDialog
                    bridgeAvailable={workspace.bridgeAvailable}
                    onCreated={(projectID) => {
                        void workspace.refresh(true)
                        setSelectedProjectID(projectID)
                        setPage('project')
                    }}
                    onClose={() => setProjectOpen(false)}
                />
            )}
            {sessionProject && (
                <NewSessionDialog
                    bridgeAvailable={workspace.bridgeAvailable}
                    projectName={sessionProject.name}
                    projectID={sessionProject.id}
                    workspaceID={sessionProject.workspaceID}
                    onCreated={selectSession}
                    onClose={() => setSessionProject(null)}
                    onOpenSettings={openSettings}
                />
            )}
        </div>
    )
}
