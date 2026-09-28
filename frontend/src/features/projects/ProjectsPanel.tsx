import { ArrowRight, ChevronDown, FolderPlus, MessageSquarePlus, Search, Settings } from 'lucide-react'
import { ProjectGroup, ProjectPanelProps } from './types'

export function ProjectsPanel({
    projectGroups,
    sessionsCount,
    selectedSessionID,
    selectedProjectID,
    lookupID,
    searchOpen,
    collapsedProjects,
    busy,
    bridgeAvailable,
    onLookupChange,
    onLoadSession,
    onToggleSearch,
    onOpenProject,
    onOpenSession,
    onSelectProject,
    onSelectSession,
    onToggleProject,
    onOpenSettings,
    settingsOpen,
}: ProjectPanelProps) {
    return (
        <aside className="sidebar projects-panel">
            <div className="sidebar-heading">
                <span className="section-label">项目</span>
                <div className="sidebar-tools">
                    <span className="sidebar-count">{projectGroups.length}</span>
                    <button
                        className="icon-button"
                        type="button"
                        title="新建项目"
                        aria-label="新建项目"
                        onClick={onOpenProject}
                        disabled={busy || !bridgeAvailable}
                    >
                        <FolderPlus size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                    <button
                        className={`icon-button ${searchOpen ? 'active' : ''}`}
                        type="button"
                        title="查找会话"
                        aria-label={searchOpen ? '关闭会话搜索' : '查找会话'}
                        aria-expanded={searchOpen}
                        onClick={onToggleSearch}
                    >
                        <Search size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
            </div>
            {searchOpen && (
                <form className="session-lookup" onSubmit={onLoadSession}>
                    <label htmlFor="session-id">通过会话 ID 查找</label>
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
                            title="加载会话"
                            aria-label="加载会话"
                            disabled={!lookupID.trim() || busy || !bridgeAvailable}
                        >
                            <ArrowRight size={16} strokeWidth={1.8} aria-hidden="true" />
                        </button>
                    </div>
                </form>
            )}
            <div className="scroll-region">
                <div className="project-list">
                    {projectGroups.map((project) => (
                        <ProjectSection
                            key={project.id}
                            project={project}
                            collapsed={collapsedProjects[project.id] === true}
                            selectedSessionID={selectedSessionID}
                            selectedProjectID={selectedProjectID}
                            busy={busy}
                            bridgeAvailable={bridgeAvailable}
                            onSelectSession={onSelectSession}
                            onSelectProject={onSelectProject}
                            onToggleProject={onToggleProject}
                            onOpenSession={onOpenSession}
                        />
                    ))}
                </div>
                {sessionsCount === 0 && projectGroups.length === 0 && <p className="muted-copy">还没有创建任何项目。</p>}
            </div>
            <div className="sidebar-footer">
                <button
                    className={`settings-nav ${settingsOpen ? 'active' : ''}`}
                    type="button"
                    title="打开设置"
                    aria-label="打开设置"
                    aria-current={settingsOpen ? 'page' : undefined}
                    onClick={onOpenSettings}
                >
                    <Settings size={16} strokeWidth={1.8} aria-hidden="true" />
                    <span>设置</span>
                </button>
            </div>
        </aside>
    )
}

type ProjectSectionProps = {
    project: ProjectGroup
    collapsed: boolean
    selectedSessionID: string
    selectedProjectID: string
    busy: boolean
    bridgeAvailable: boolean
    onSelectSession: (id: string) => void
    onSelectProject: (project: ProjectGroup) => void
    onToggleProject: (id: string) => void
    onOpenSession: (project: ProjectGroup) => void
}

function ProjectSection({
    project,
    collapsed,
    selectedSessionID,
    selectedProjectID,
    busy,
    bridgeAvailable,
    onSelectSession,
    onSelectProject,
    onToggleProject,
    onOpenSession,
}: ProjectSectionProps) {
    const sessionListID = projectSessionID(project.id)
    return (
        <section className="project-section" key={project.id}>
            <div className={`project-heading ${project.id === selectedProjectID || project.sessions.some((item) => item.id === selectedSessionID) ? 'selected' : ''}`}>
                <button
                    className="project-toggle"
                    type="button"
                    title={`${collapsed ? '展开' : '折叠'} ${project.name} (${project.path})`}
                    aria-label={`${collapsed ? '展开' : '折叠'}项目 ${project.name}`}
                    aria-expanded={!collapsed}
                    aria-controls={sessionListID}
                    onClick={() => onToggleProject(project.id)}
                >
                    <span className={`project-chevron ${collapsed ? 'collapsed' : ''}`} aria-hidden="true">
                        <ChevronDown size={14} strokeWidth={2.2} />
                    </span>
                </button>
                <button
                    className="project-select"
                    type="button"
                    title={`打开项目 ${project.name}`}
                    aria-label={`打开项目 ${project.name}`}
                    onClick={() => onSelectProject(project)}
                >
                    <span className="project-heading-copy">
                        <strong>{project.name}</strong>
                        <small>{project.path}</small>
                    </span>
                </button>
                <span className="project-count">{project.sessions.length}</span>
                <button
                    className="icon-button project-session-button"
                    type="button"
                    title={`在 ${project.name} 中新建会话`}
                    aria-label={`在 ${project.name} 中新建会话`}
                    onClick={() => onOpenSession(project)}
                    disabled={busy || !bridgeAvailable}
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
                            <strong>{item.title || '未命名会话'}</strong>
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
