import {FormEvent, useEffect, useMemo, useState} from 'react'
import {AgentsPanel} from './AgentsPanel'
import {NewProjectDialog} from './NewProjectDialog'
import {NewSessionDialog} from './NewSessionDialog'
import {ProjectsPanel} from './ProjectsPanel'
import {projectNameFromWorkspace, groupSessions} from './projectGroups'
import {SessionPanel} from './SessionPanel'
import {useProjectWorkspace} from './useProjectWorkspace'
import {WorkspaceHeader} from './WorkspaceHeader'

export function ProjectWorkspace() {
    const workspace = useProjectWorkspace()
    const [lookupID, setLookupID] = useState(workspace.selectedSessionID)
    const [searchOpen, setSearchOpen] = useState(false)
    const [projectOpen, setProjectOpen] = useState(false)
    const [sessionWorkspaceKey, setSessionWorkspaceKey] = useState('')
    const [collapsedProjects, setCollapsedProjects] = useState<Record<string, boolean>>({})
    const projectGroups = useMemo(() => groupSessions(workspace.sessions), [workspace.sessions])

    useEffect(() => {
        setLookupID(workspace.selectedSessionID)
    }, [workspace.selectedSessionID])

    const selectSession = (id: string) => {
        setLookupID(id)
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
        setProjectOpen(true)
    }

    const openSession = (workspaceKey: string) => {
        workspace.clearError()
        setSessionWorkspaceKey(workspaceKey)
    }

    return (
        <div className="workspace">
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
                    onSelectSession={selectSession}
                    onToggleProject={(key) => setCollapsedProjects((current) => ({
                        ...current,
                        [key]: !current[key],
                    }))}
                />

                <main className="main-panel">
                    {workspace.error && <div className="error-banner" role="alert">{workspace.error}</div>}
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
					selectedModelID={workspace.selectedModelID}
					reasoning={workspace.reasoning}
                        busy={workspace.busy}
                        onSend={() => void workspace.submitInput()}
                        onControl={(kind) => void workspace.control(kind)}
					onModelChange={workspace.selectModel}
					onReasoningChange={workspace.selectReasoning}
                    />
                </main>

                <AgentsPanel
                    loading={Boolean(workspace.selectedSessionID && !workspace.session)}
                    session={workspace.session}
                    selectedAgentID={workspace.selectedAgentID}
                    onSelectAgent={(id) => void workspace.selectAgent(id)}
                />
            </div>

            {projectOpen && (
                <NewProjectDialog
                    bridgeAvailable={workspace.bridgeAvailable}
                    onCreated={selectSession}
                    onClose={() => setProjectOpen(false)}
                />
            )}
            {sessionWorkspaceKey && (
                <NewSessionDialog
                    bridgeAvailable={workspace.bridgeAvailable}
                    projectName={projectNameFromWorkspace(sessionWorkspaceKey)}
                    workspaceKey={sessionWorkspaceKey}
                    onCreated={selectSession}
                    onClose={() => setSessionWorkspaceKey('')}
                />
            )}
        </div>
    )
}
