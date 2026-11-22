import { ArrowRight, ChevronDown, FolderPlus, MessageSquarePlus, PanelLeftClose, Search, X } from 'lucide-react'
import type { ProjectGroup, ProjectPanelProps } from './types'
import { SessionActions } from './SessionActions'

export function ProjectsPanel({
    projectGroups,
    sessionsCount,
    selectedSessionID,
    selectedProjectID,
    activeProjectID,
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
    onDeleteSession,
    onRenameSession,
    onRevealSessionFile,
    onCloseNavigation,
}: ProjectPanelProps) {
    const query = searchOpen ? lookupID.trim().toLowerCase() : ''
    const filteredGroups = query
        ? projectGroups.flatMap((project) => {
            const projectMatches = project.name.toLowerCase().includes(query)
                || project.path.toLowerCase().includes(query)
                || project.id.toLowerCase().includes(query)
            const matchedSessions = project.sessions.filter((item) => (
                (item.title || '未命名会话').toLowerCase().includes(query)
                || item.id.toLowerCase().includes(query)
            ))
            if (matchedSessions.length > 0) return [{ ...project, sessions: matchedSessions }]
            if (projectMatches) return [project]
            return []
        })
        : projectGroups

    return (
        <aside className="sidebar side projects-panel" id="workspace-navigation" aria-labelledby="workspace-navigation-title">
            <div className="sidebar-heading side-head">
                <span className="section-label" id="workspace-navigation-title">项目</span>
                <div className="sidebar-tools tools">
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
                        title="筛选项目或按会话 ID 打开"
                        aria-label={searchOpen ? '关闭项目与会话搜索' : '搜索项目或会话'}
                        aria-expanded={searchOpen}
                        onClick={onToggleSearch}
                    >
                        <Search size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                    {onCloseNavigation && (
                        <button className="icon-button" type="button" title="收起项目目录 (Ctrl+K)" aria-label="收起项目目录" onClick={onCloseNavigation}>
                            <PanelLeftClose size={16} strokeWidth={1.8} aria-hidden="true" />
                        </button>
                    )}
                </div>
            </div>
            {searchOpen && (
                <form className="session-lookup" onSubmit={onLoadSession}>
                    <label htmlFor="session-id">筛选项目 / 会话，或输入会话 ID 直达</label>
                    <div className="lookup-row">
                        <div className="lookup-input-wrap">
                            <input
                                id="session-id"
                                value={lookupID}
                                onChange={(event) => onLookupChange(event.target.value)}
                                placeholder="名称、路径或会话 ID"
                                autoComplete="off"
                                autoFocus
                            />
                            {lookupID && (
                                <button
                                    className="icon-button lookup-clear"
                                    type="button"
                                    title="清空搜索"
                                    aria-label="清空搜索"
                                    onClick={() => onLookupChange('')}
                                >
                                    <X size={13} strokeWidth={1.8} aria-hidden="true" />
                                </button>
                            )}
                        </div>
                        <button
                            className="lookup-button"
                            type="submit"
                            title="按 ID 打开会话"
                            aria-label="按 ID 打开会话"
                            disabled={!lookupID.trim() || busy || !bridgeAvailable}
                        >
                            <ArrowRight size={16} strokeWidth={1.8} aria-hidden="true" />
                        </button>
                    </div>
                </form>
            )}
            <div className="scroll-region scroll">
                <div className="project-list">
                    {filteredGroups.map((project) => (
                        <ProjectSection
                            key={project.id}
                            project={project}
                            collapsed={!query && collapsedProjects[project.id] === true}
                            selectedSessionID={selectedSessionID}
                            selectedProjectID={selectedProjectID}
                            activeProjectID={activeProjectID}
                            busy={busy}
                            bridgeAvailable={bridgeAvailable}
                            onSelectSession={onSelectSession}
                            onSelectProject={onSelectProject}
                            onToggleProject={onToggleProject}
                            onOpenSession={onOpenSession}
                            onDeleteSession={onDeleteSession}
                            onRenameSession={onRenameSession}
                            onRevealSessionFile={onRevealSessionFile}
                        />
                    ))}
                </div>
                {sessionsCount === 0 && projectGroups.length === 0 ? (
                    <div className="projects-empty-state">
                        <p className="muted-copy">还没有绑定任何项目目录。</p>
                        <button
                            className="quiet-button"
                            type="button"
                            onClick={onOpenProject}
                            disabled={busy || !bridgeAvailable}
                        >
                            <FolderPlus size={14} strokeWidth={1.8} aria-hidden="true" />
                            <span>新建项目</span>
                        </button>
                    </div>
                ) : query && filteredGroups.length === 0 ? (
                    <p className="muted-copy">没有匹配的项目或会话，可按右侧箭头尝试按 ID 打开。</p>
                ) : null}
            </div>
        </aside>
    )
}

type ProjectSectionProps = {
    project: ProjectGroup
    collapsed: boolean
    selectedSessionID: string
    selectedProjectID: string
    activeProjectID: string
    busy: boolean
    bridgeAvailable: boolean
    onSelectSession: (id: string) => void
    onSelectProject: (project: ProjectGroup) => void
    onToggleProject: (id: string) => void
    onOpenSession: (project: ProjectGroup) => void
    onDeleteSession: (id: string) => Promise<boolean>
    onRenameSession: (id: string, title: string) => Promise<boolean>
    onRevealSessionFile: (id: string) => Promise<boolean>
}

function ProjectSection({
    project,
    collapsed,
    selectedSessionID,
    selectedProjectID,
    activeProjectID,
    busy,
    bridgeAvailable,
    onSelectSession,
    onSelectProject,
    onToggleProject,
    onOpenSession,
    onDeleteSession,
    onRenameSession,
    onRevealSessionFile,
}: ProjectSectionProps) {
    const sessionListID = `project-sessions-${encodeURIComponent(project.id)}`
    const selected = project.id === selectedProjectID
    const hasActiveSession = project.id === activeProjectID || project.sessions.some((item) => item.id === selectedSessionID)
    return (
        <section className="project-section proj">
            <div className={`project-heading proj-row ${selected ? 'selected' : hasActiveSession ? 'has-active-session' : ''}`}>
                <button
                    className="project-toggle"
                    type="button"
                    title={`${collapsed ? '展开' : '折叠'} ${project.name} (${project.path})`}
                    aria-label={`${collapsed ? '展开' : '折叠'}项目 ${project.name}`}
                    aria-expanded={!collapsed}
                    aria-controls={sessionListID}
                    onClick={() => onToggleProject(project.id)}
                >
                    <span className={`project-chevron chev ${collapsed ? 'collapsed' : ''}`} aria-hidden="true">
                        <ChevronDown size={14} strokeWidth={2.2} />
                    </span>
                </button>
                <button
                    className="project-select"
                    type="button"
                    title={`${project.name} (${project.path})`}
                    aria-label={`打开项目 ${project.name}`}
                    onClick={() => onSelectProject(project)}
                >
                    <span className="project-heading-copy">
                        <strong className="name">{project.name}</strong>
                        <small>{project.path}</small>
                    </span>
                </button>
                <span className="project-count count">{project.sessions.length}</span>
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
            <div className="project-path" title={project.path}>{project.path}</div>
            <div className={`session-list ${collapsed ? 'collapsed' : ''}`} id={sessionListID} hidden={collapsed}>
                {project.sessions.map((item) => {
                    const sessionTitle = item.title || '未命名会话'
                    return (
                        <div className={`session-row sess ${item.id === selectedSessionID ? 'selected' : ''}`} key={item.id}>
                            <button className="session-row-select" type="button" title={sessionTitle} onClick={() => onSelectSession(item.id)}>
                                <span className="session-row-copy">
                                    <strong className="name">{sessionTitle}</strong>
                                </span>
                            </button>
                            <SessionActions
                                title={item.title}
                                busy={busy}
                                disabled={busy || !bridgeAvailable || collapsed}
                                onDelete={() => onDeleteSession(item.id)}
                                onRename={(title) => onRenameSession(item.id, title)}
                                onRevealFile={() => onRevealSessionFile(item.id)}
                            />
                        </div>
                    )
                })}
            </div>
        </section>
    )
}
