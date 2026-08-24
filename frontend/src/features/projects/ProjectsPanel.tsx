import {ArrowRight, ChevronDown, FolderPlus, MessageSquarePlus, Search} from 'lucide-react'
import {ProjectGroup, ProjectPanelProps} from './types'

export function ProjectsPanel({
    projectGroups,
    sessionsCount,
    selectedSessionID,
    lookupID,
    searchOpen,
    collapsedProjects,
    busy,
    bridgeAvailable,
    healthReady,
    onLookupChange,
    onLoadSession,
    onToggleSearch,
    onOpenProject,
    onOpenSession,
    onSelectSession,
    onToggleProject,
}: ProjectPanelProps) {
    return (
        <aside className="sidebar projects-panel">
            <div className="sidebar-heading">
                <span className="section-label">Projects</span>
                <div className="sidebar-tools">
                    <span className="sidebar-count">{projectGroups.length}</span>
                    <button
                        className="icon-button"
                        type="button"
                        title="New project"
                        aria-label="New project"
                        onClick={onOpenProject}
                        disabled={busy || !bridgeAvailable || !healthReady}
                    >
                        <FolderPlus size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                    <button
                        className={`icon-button ${searchOpen ? 'active' : ''}`}
                        type="button"
                        title="Find session"
                        aria-label={searchOpen ? 'Close session search' : 'Find session'}
                        aria-expanded={searchOpen}
                        onClick={onToggleSearch}
                    >
                        <Search size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
            </div>
            {searchOpen && (
                <form className="session-lookup" onSubmit={onLoadSession}>
                    <label htmlFor="session-id">Find by session ID</label>
                    <div className="lookup-row">
                        <input
                            id="session-id"
                            value={lookupID}
                            onChange={(event) => onLookupChange(event.target.value)}
                            placeholder="session_..."
                            autoComplete="off"
                            autoFocus
                        />
                        <button
                            className="lookup-button"
                            type="submit"
                            title="Load session"
                            aria-label="Load session"
                            disabled={!lookupID.trim() || busy || !bridgeAvailable}
                        >
                            <ArrowRight size={16} strokeWidth={1.8} aria-hidden="true" />
                        </button>
                    </div>
                </form>
            )}
            <div className="project-list">
                {projectGroups.map((project) => (
                    <ProjectSection
                        key={project.key}
                        project={project}
                        collapsed={collapsedProjects[project.key] === true}
                        selectedSessionID={selectedSessionID}
                        busy={busy}
                        bridgeAvailable={bridgeAvailable}
                        healthReady={healthReady}
                        onSelectSession={onSelectSession}
                        onToggleProject={onToggleProject}
                        onOpenSession={onOpenSession}
                    />
                ))}
            </div>
            {sessionsCount === 0 && <p className="muted-copy">No sessions have been created yet.</p>}
        </aside>
    )
}

type ProjectSectionProps = {
    project: ProjectGroup
    collapsed: boolean
    selectedSessionID: string
    busy: boolean
    bridgeAvailable: boolean
    healthReady: boolean
    onSelectSession: (id: string) => void
    onToggleProject: (key: string) => void
    onOpenSession: (workspaceKey: string) => void
}

function ProjectSection({
    project,
    collapsed,
    selectedSessionID,
    busy,
    bridgeAvailable,
    healthReady,
    onSelectSession,
    onToggleProject,
    onOpenSession,
}: ProjectSectionProps) {
    const sessionListID = projectSessionID(project.key)
    return (
        <section className="project-section" key={project.key}>
            <div className="project-heading">
                <button
                    className="project-toggle"
                    type="button"
                    title={`${collapsed ? 'Expand' : 'Collapse'} ${project.name} (${project.path})`}
                    aria-label={`${collapsed ? 'Expand' : 'Collapse'} project ${project.name}`}
                    aria-expanded={!collapsed}
                    aria-controls={sessionListID}
                    onClick={() => onToggleProject(project.key)}
                >
                    <span className={`project-chevron ${collapsed ? 'collapsed' : ''}`} aria-hidden="true">
                        <ChevronDown size={14} strokeWidth={2.2} />
                    </span>
                    <span className="project-heading-copy">
                        <strong>{project.name}</strong>
                        <small>{project.path}</small>
                    </span>
                </button>
                <span className="project-count">{project.sessions.length}</span>
                <button
                    className="icon-button project-session-button"
                    type="button"
                    title={`New session in ${project.name}`}
                    aria-label={`New session in ${project.name}`}
                    onClick={() => onOpenSession(project.path)}
                    disabled={busy || !bridgeAvailable || !healthReady}
                >
                    <MessageSquarePlus size={15} strokeWidth={1.8} aria-hidden="true" />
                </button>
            </div>
            <div className={`session-list ${collapsed ? 'collapsed' : ''}`} id={sessionListID} hidden={collapsed}>
                {project.sessions.map((item) => (
                    <button
                        className={`session-row ${item.id === selectedSessionID ? 'selected' : ''}`}
                        key={item.id}
                        type="button"
                        onClick={() => onSelectSession(item.id)}
                    >
                        <span className="session-row-copy">
                            <strong>{item.goal || 'Untitled session'}</strong>
                        </span>
                    </button>
                ))}
            </div>
        </section>
    )
}

function projectSessionID(key: string) {
    return `project-sessions-${encodeURIComponent(key)}`
}
