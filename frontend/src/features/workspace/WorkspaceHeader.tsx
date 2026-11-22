import { useEffect, useRef } from 'react'
import { FolderOpen, Globe, Layers, Settings2, X } from 'lucide-react'
import type { SubagentProgressSummary } from './useSessionPage'
import type { WorkspacePage } from './WorkspaceRail'

export type WorkspaceSessionTab = {
    id: string
    title: string
    status: string
    queuedCount: number
    busy: boolean
}

type WorkspaceHeaderProps = {
    activePage: WorkspacePage
    selectedSessionID: string
    tabs: WorkspaceSessionTab[]
    projectName?: string
    navigationOpen: boolean
    busy: boolean
    refreshing: boolean
    subagentSummary?: SubagentProgressSummary
    onSelectSession: (id: string) => void
    onRequestCloseSession: (id: string) => void
    onToggleNavigation: () => void
    onOpenOverview?: () => void
}

/** 42px 一体化顶栏：左侧承载已打开的会话标签与当前页面上下文，右侧承载全局子代理进度与状态。 */
export function WorkspaceHeader({
    activePage,
    selectedSessionID,
    tabs,
    projectName,
    navigationOpen,
    busy,
    refreshing,
    subagentSummary,
    onSelectSession,
    onRequestCloseSession,
    onToggleNavigation,
    onOpenOverview,
}: WorkspaceHeaderProps) {
    const tabsRef = useRef<HTMLDivElement>(null)
    const hasSubagents = Boolean(activePage === 'session' && subagentSummary && subagentSummary.total > 0 && onOpenOverview)
    const totalDenom = Math.max(1, subagentSummary?.total ?? 1)

    useEffect(() => {
        const area = tabsRef.current
        if (!area) return
        const handleWheel = (event: WheelEvent) => {
            if (Math.abs(event.deltaY) > Math.abs(event.deltaX) && area.scrollWidth > area.clientWidth) {
                area.scrollLeft += event.deltaY
                event.preventDefault()
            }
        }
        area.addEventListener('wheel', handleWheel, { passive: false })
        return () => area.removeEventListener('wheel', handleWheel)
    }, [])

    return (
        <header className="topbar">
            <nav className="topbar-tabs-area" ref={tabsRef} aria-label="已打开的会话与当前视图">
                {activePage === 'world' ? (
                    <span className="topbar-page-tab is-active" aria-current="page">
                        <Globe size={13} strokeWidth={1.9} aria-hidden="true" />
                        <span>共生推演世界</span>
                    </span>
                ) : (
                    <>
                        {tabs.map((tab) => {
                            const isActive = activePage === 'session' && selectedSessionID === tab.id
                            return (
                                <div className="session-tab" key={tab.id} data-active={isActive} data-status={tab.status}>
                                    <button
                                        type="button"
                                        className="session-tab-select"
                                        aria-current={isActive ? 'page' : undefined}
                                        onClick={() => onSelectSession(tab.id)}
                                        title={`${tab.title}${tab.queuedCount > 0 ? ` · ${tab.queuedCount} 个待办` : ''}`}
                                    >
                                        {tab.status !== 'idle' && <span className={`session-tab-dot is-${tab.status}`} aria-hidden="true" />}
                                        <span className="session-tab-label">{tab.title}</span>
                                        {tab.queuedCount > 0 && (
                                            <span className="session-tab-queue" title={`${tab.queuedCount} 个待执行任务`}>
                                                {tab.queuedCount}
                                            </span>
                                        )}
                                    </button>
                                    <button
                                        type="button"
                                        className="icon-button session-tab-close"
                                        disabled={tab.busy}
                                        onClick={() => onRequestCloseSession(tab.id)}
                                        aria-label={`关闭会话页：${tab.title}`}
                                        title="关闭会话页"
                                    >
                                        <X size={13} aria-hidden="true" />
                                    </button>
                                </div>
                            )
                        })}

                        {activePage === 'project' && (
                            <>
                                {tabs.length > 0 && <span className="topbar-sep" aria-hidden="true" />}
                                <span className="topbar-page-tab is-active" aria-current="page">
                                    <FolderOpen size={13} strokeWidth={1.9} aria-hidden="true" />
                                    <span>项目概览{projectName ? ` · ${projectName}` : ''}</span>
                                </span>
                            </>
                        )}

                        {activePage === 'settings' && (
                            <>
                                {tabs.length > 0 && <span className="topbar-sep" aria-hidden="true" />}
                                <span className="topbar-page-tab is-active" aria-current="page">
                                    <Settings2 size={13} strokeWidth={1.9} aria-hidden="true" />
                                    <span>工作区设置</span>
                                </span>
                            </>
                        )}

                        {activePage === 'session' && tabs.length === 0 && (
                            <>
                                <span className="topbar-page-tab is-active" aria-current="page">
                                    <Layers size={13} strokeWidth={1.9} aria-hidden="true" />
                                    <span>工作台</span>
                                </span>
                                {!navigationOpen && (
                                    <button
                                        className="topbar-quick-nav"
                                        type="button"
                                        title="展开项目目录（Ctrl+K）"
                                        onClick={onToggleNavigation}
                                    >
                                        <span>项目目录</span>
                                        <kbd>Ctrl+K</kbd>
                                    </button>
                                )}
                            </>
                        )}
                    </>
                )}
            </nav>

            <div className="topbar-right">
                {hasSubagents && subagentSummary && onOpenOverview ? (
                    <button
                        className="topbar-progress-btn"
                        type="button"
                        title="点击切换到右侧「总览」面板"
                        aria-label={`子代理进度 ${subagentSummary.done} / ${subagentSummary.total}，点击打开总览`}
                        onClick={onOpenOverview}
                    >
                        <i className={`dot ${subagentSummary.live > 0 ? 'live' : subagentSummary.waiting > 0 ? 'warn' : ''}`} aria-hidden="true" />
                        <span className="topbar-mini-bar" aria-hidden="true">
                            {subagentSummary.done > 0 && <i className="is-done" style={{ width: `${(subagentSummary.done / totalDenom) * 100}%` }} />}
                            {subagentSummary.live > 0 && <i className="is-live" style={{ width: `${(subagentSummary.live / totalDenom) * 100}%` }} />}
                            {subagentSummary.waiting > 0 && <i className="is-warn" style={{ width: `${(subagentSummary.waiting / totalDenom) * 100}%` }} />}
                            {subagentSummary.failed > 0 && <i className="is-fail" style={{ width: `${(subagentSummary.failed / totalDenom) * 100}%` }} />}
                        </span>
                        <span className="topbar-progress-ratio">{subagentSummary.done}/{subagentSummary.total}</span>
                        <span className="topbar-progress-detail">
                            {subagentSummary.live > 0 ? `${subagentSummary.live} 运行` : '已就绪'}
                            {subagentSummary.waiting > 0 ? ` · ${subagentSummary.waiting} 待审` : ''}
                            {subagentSummary.failed > 0 ? ` · ${subagentSummary.failed} 异常` : ''}
                        </span>
                    </button>
                ) : (
                    <div className="status topbar-status">
                        <i className={`dot ${busy || refreshing ? 'live' : ''}`} aria-hidden="true" />
                        <span>{refreshing ? '刷新中' : busy ? '处理中' : '已就绪'}</span>
                    </div>
                )}
            </div>
        </header>
    )
}
