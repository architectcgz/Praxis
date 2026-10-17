import { useLayoutEffect, useRef, useState } from 'react'
import { ArrowRight, ChevronDown, Ellipsis, FolderOpen, FolderPlus, MessageSquarePlus, Pencil, Search, Settings, Trash2, X } from 'lucide-react'
import { Overlay } from '../../components/ui'
import { ProjectGroup, ProjectPanelProps } from './types'
import { sessionMenuPlacement } from './sessionMenuPlacement'

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
    onDeleteSession,
    onRenameSession,
    onOpenSettings,
    onRevealSessionFile,
    settingsOpen,
    onCloseNavigation,
}: ProjectPanelProps) {
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
                        title="查找会话"
                        aria-label={searchOpen ? '关闭会话搜索' : '查找会话'}
                        aria-expanded={searchOpen}
                        onClick={onToggleSearch}
                    >
                        <Search size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                    {onCloseNavigation && (
                        <button className="icon-button" type="button" title="关闭项目导航" aria-label="关闭项目导航" onClick={onCloseNavigation}>
                            <X size={18} strokeWidth={1.8} aria-hidden="true" />
                        </button>
                    )}
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
            <div className="scroll-region scroll">
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
                            onDeleteSession={onDeleteSession}
                            onRenameSession={onRenameSession}
                            onRevealSessionFile={onRevealSessionFile}
                        />
                    ))}
                </div>
                {sessionsCount === 0 && projectGroups.length === 0 && <p className="muted-copy">还没有创建任何项目。</p>}
            </div>
            <div className="sidebar-footer foot">
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
    onDeleteSession: (id: string) => Promise<boolean>
    onRenameSession: (id: string, title: string) => Promise<boolean>
    onRevealSessionFile: (id: string) => Promise<boolean>
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
    onDeleteSession,
    onRenameSession,
    onRevealSessionFile,
}: ProjectSectionProps) {
    const [openSessionMenuID, setOpenSessionMenuID] = useState('')
    const [pendingDelete, setPendingDelete] = useState<{ id: string; title: string } | null>(null)
    const [pendingRename, setPendingRename] = useState<{ id: string; title: string } | null>(null)
    const menuRef = useRef<HTMLDivElement>(null)
    const menuTriggerRef = useRef<HTMLButtonElement>(null)
    const sessionListID = projectSessionID(project.id)

    useLayoutEffect(() => {
        const menu = menuRef.current
        const anchor = menu?.parentElement
        const scroll = anchor?.closest<HTMLElement>('.scroll-region')
        const trigger = anchor?.querySelector<HTMLButtonElement>('.session-menu-trigger')
        if (!menu || !anchor || !scroll || !trigger) return
        menuTriggerRef.current = trigger
        const closeMenu = (restoreFocus = false) => {
            if (restoreFocus) trigger.focus({ preventScroll: true })
            setOpenSessionMenuID('')
        }
        if (collapsed || busy || !bridgeAvailable) {
            closeMenu()
            return
        }
        const updatePlacement = () => {
            const rect = scroll.getBoundingClientRect()
            const top = Math.max(0, rect.top + scroll.clientTop)
            const bottom = Math.min(window.innerHeight, rect.top + scroll.clientTop + scroll.clientHeight)
            const placement = sessionMenuPlacement(anchor.getBoundingClientRect(), { top, bottom }, menu.scrollHeight + menu.offsetHeight - menu.clientHeight)
            if (scroll.clientWidth === 0 || !placement) {
                closeMenu()
                return
            }
            menu.dataset.placement = placement.placement
            menu.style.maxHeight = `${placement.maxHeight}px`
            menu.style.visibility = 'visible'
        }
        // 定位在首次绘制前完成，菜单不扩张侧栏的滚动范围。
        updatePlacement()
        menu.querySelector<HTMLButtonElement>('button')?.focus({ preventScroll: true })
        const closeOnOutsidePress = (event: PointerEvent) => {
            if (event.target instanceof Node && !anchor.contains(event.target)) closeMenu()
        }
        const closeOnScroll = () => closeMenu(menu.contains(document.activeElement))
        const onKeyDown = (event: KeyboardEvent) => {
            if (!anchor.contains(document.activeElement)) return
            if (event.key === 'Escape' || event.key === 'Tab') {
                if (event.key === 'Escape') {
                    event.preventDefault()
                    event.stopPropagation()
                }
                closeMenu(true)
                return
            }
            const items = Array.from(menu.querySelectorAll<HTMLButtonElement>('[role="menuitem"]'))
            const index = items.indexOf(document.activeElement as HTMLButtonElement)
            const next = event.key === 'ArrowDown' ? (index + 1) % items.length
                : event.key === 'ArrowUp' ? (index - 1 + items.length) % items.length
                : event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 : -1
            if (next >= 0) {
                event.preventDefault()
                items[next].focus()
            }
        }
        const observer = new ResizeObserver(updatePlacement)
        observer.observe(scroll)
        scroll.addEventListener('scroll', closeOnScroll)
        window.addEventListener('resize', updatePlacement)
        document.addEventListener('pointerdown', closeOnOutsidePress, true)
        document.addEventListener('keydown', onKeyDown)
        return () => {
            observer.disconnect()
            scroll.removeEventListener('scroll', closeOnScroll)
            window.removeEventListener('resize', updatePlacement)
            document.removeEventListener('pointerdown', closeOnOutsidePress, true)
            document.removeEventListener('keydown', onKeyDown)
        }
    }, [openSessionMenuID, collapsed, busy, bridgeAvailable])

    const restoreMenuTriggerFocus = () => {
        menuTriggerRef.current?.focus({ preventScroll: true })
    }

    const requestDeleteSession = (id: string, title: string) => {
        restoreMenuTriggerFocus()
        setOpenSessionMenuID('')
        setPendingDelete({ id, title: title || '未命名会话' })
    }

    const requestRenameSession = (id: string, title: string) => {
        restoreMenuTriggerFocus()
        setOpenSessionMenuID('')
        setPendingRename({ id, title })
    }
    return (
        <section className="project-section proj" key={project.id}>
            <div className={`project-heading proj-row ${project.id === selectedProjectID || project.sessions.some((item) => item.id === selectedSessionID) ? 'selected' : ''}`}>
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
                    title={`打开项目 ${project.name}`}
                    aria-label={`打开项目 ${project.name}`}
                    onClick={() => onSelectProject(project)}
                >
                    <span className="project-heading-copy">
                        <strong className="name">{truncateName(project.name)}</strong>
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
            <div className="project-path">{project.path}</div>
            <div className={`session-list ${collapsed ? 'collapsed' : ''}`} id={sessionListID} hidden={collapsed}>
                {project.sessions.map((item) => (
                    <div
                        className={`session-row sess ${item.id === selectedSessionID ? 'selected' : ''}`}
                        key={item.id}
                    >
                        <button className="session-row-select" type="button" onClick={() => onSelectSession(item.id)}>
                            <span className="session-row-copy">
                                <strong className="name">{truncateName(item.title || '未命名会话')}</strong>
                            </span>
                        </button>
                        <div className="session-row-actions" onBlur={(event) => {
                            if (!event.currentTarget.contains(event.relatedTarget)) setOpenSessionMenuID('')
                        }}>
                            <button
                                className="icon-button session-menu-trigger"
                                type="button"
                                title="会话操作"
                                aria-label={`打开会话 ${item.title || '未命名会话'} 操作`}
                                aria-haspopup="menu"
                                aria-expanded={openSessionMenuID === item.id}
                                aria-controls={openSessionMenuID === item.id ? `session-actions-${encodeURIComponent(item.id)}` : undefined}
                                onClick={() => setOpenSessionMenuID((current) => current === item.id ? '' : item.id)}
                                onKeyDown={(event) => {
                                    if (event.key === 'ArrowDown') {
                                        event.preventDefault()
                                        event.stopPropagation()
                                        setOpenSessionMenuID(item.id)
                                        if (openSessionMenuID === item.id) menuRef.current?.querySelector<HTMLButtonElement>('button')?.focus()
                                    }
                                }}
                                disabled={busy || !bridgeAvailable}
                            >
                                <Ellipsis size={15} strokeWidth={1.8} aria-hidden="true" />
                            </button>
                            {openSessionMenuID === item.id && (
                                <div className="session-actions-menu" role="menu" aria-label="会话操作" id={`session-actions-${encodeURIComponent(item.id)}`} ref={menuRef}>
                                    <button
                                        type="button"
                                        role="menuitem"
                                        tabIndex={-1}
                                        className="session-action"
                                        onClick={() => requestRenameSession(item.id, item.title)}
                                    >
                                        <Pencil size={14} strokeWidth={1.8} aria-hidden="true" />
                                        <span>重命名会话</span>
                                    </button>
                                    <button
                                        type="button"
                                        role="menuitem"
                                        tabIndex={-1}
                                        className="session-action"
                                        onClick={() => {
                                            restoreMenuTriggerFocus()
                                            setOpenSessionMenuID('')
                                            void onRevealSessionFile(item.id)
                                        }}
                                    >
                                        <FolderOpen size={14} strokeWidth={1.8} aria-hidden="true" />
                                        <span>打开所在位置</span>
                                    </button>
                                    <button
                                        type="button"
                                        role="menuitem"
                                        tabIndex={-1}
                                        className="session-action-danger"
                                        onClick={() => requestDeleteSession(item.id, item.title)}
                                    >
                                        <Trash2 size={14} strokeWidth={1.8} aria-hidden="true" />
                                        <span>删除会话</span>
                                    </button>
                                </div>
                            )}
                        </div>
                    </div>
                ))}
            </div>
            {pendingDelete && (
                <DeleteSessionDialog
                    title={pendingDelete.title}
                    busy={busy}
                    onClose={() => {
                        setPendingDelete(null)
                        restoreMenuTriggerFocus()
                    }}
                    onConfirm={() => {
                        setPendingDelete(null)
                        restoreMenuTriggerFocus()
                        return onDeleteSession(pendingDelete.id)
                    }}
                />
            )}
            {pendingRename && (
                <RenameSessionDialog
                    initialTitle={pendingRename.title}
                    busy={busy}
                    onClose={() => {
                        setPendingRename(null)
                        restoreMenuTriggerFocus()
                    }}
                    onSubmit={async (title) => {
                        const renamed = await onRenameSession(pendingRename.id, title)
                        if (renamed) {
                            setPendingRename(null)
                            restoreMenuTriggerFocus()
                        }
                    }}
                />
            )}
        </section>
    )
}

type DeleteSessionDialogProps = {
    title: string
    busy: boolean
    onClose: () => void
    onConfirm: () => Promise<boolean>
}

function DeleteSessionDialog({ title, busy, onClose, onConfirm }: DeleteSessionDialogProps) {
    return (
        <Overlay labelledBy="delete-session-title" onClose={() => { if (!busy) onClose() }}>
            <section className="project-dialog session-dialog" aria-describedby="delete-session-message">
                <div className="dialog-heading">
                    <div>
                        <span className="section-label">会话操作</span>
                        <h2 id="delete-session-title">删除会话</h2>
                    </div>
                    <button
                        className="icon-button dialog-close"
                        type="button"
                        title="关闭"
                        aria-label="关闭删除会话对话框"
                        onClick={onClose}
                        disabled={busy}
                    >
                        <X size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
                <p className="session-dialog-message" id="delete-session-message">
                    确定删除会话“{title}”吗？此操作不可恢复。
                </p>
                <div className="modal-actions">
                    <button className="modal-secondary" type="button" onClick={onClose} disabled={busy}>取消</button>
                    <button className="modal-primary modal-danger" type="button" onClick={() => void onConfirm()} disabled={busy}>删除会话</button>
                </div>
            </section>
        </Overlay>
    )
}

type RenameSessionDialogProps = {
    initialTitle: string
    busy: boolean
    onClose: () => void
    onSubmit: (title: string) => Promise<void>
}

function RenameSessionDialog({ initialTitle, busy, onClose, onSubmit }: RenameSessionDialogProps) {
    const [title, setTitle] = useState(initialTitle)

    return (
        <Overlay labelledBy="rename-session-title" onClose={() => { if (!busy) onClose() }}>
            <section className="project-dialog session-dialog">
                <div className="dialog-heading">
                    <div>
                        <span className="section-label">会话操作</span>
                        <h2 id="rename-session-title">重命名会话</h2>
                    </div>
                    <button
                        className="icon-button dialog-close"
                        type="button"
                        title="关闭"
                        aria-label="关闭重命名会话对话框"
                        onClick={onClose}
                        disabled={busy}
                    >
                        <X size={16} strokeWidth={1.8} aria-hidden="true" />
                    </button>
                </div>
                <form className="project-form" onSubmit={(event) => { event.preventDefault(); void onSubmit(title.trim()) }}>
                    <label htmlFor="rename-session-input">会话名称</label>
                    <input
                        id="rename-session-input"
                        value={title}
                        onChange={(event) => setTitle(event.target.value)}
                        autoComplete="off"
                        autoFocus
                        required
                        disabled={busy}
                    />
                    <div className="modal-actions">
                        <button className="modal-secondary" type="button" onClick={onClose} disabled={busy}>取消</button>
                        <button className="modal-primary" type="submit" disabled={busy || !title.trim()}>保存</button>
                    </div>
                </form>
            </section>
        </Overlay>
    )
}

function projectSessionID(key: string) {
    return `project-sessions-${encodeURIComponent(key)}`
}

function truncateName(name: string) {
    const characters = Array.from(name)
    return characters.length > 12 ? `${characters.slice(0, 12).join('')}...` : name
}
