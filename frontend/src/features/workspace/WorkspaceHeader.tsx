import { Menu, RefreshCw } from 'lucide-react'

type WorkspaceHeaderProps = {
    busy: boolean
    refreshing: boolean
    projectName?: string
    sessionTitle?: string
    onRefresh: () => void
    navigationOpen?: boolean
    onOpenNavigation?: () => void
}

export function WorkspaceHeader({ busy, refreshing, projectName, sessionTitle, onRefresh, navigationOpen = false, onOpenNavigation }: WorkspaceHeaderProps) {
    return (
        <header className="topbar top">
            {onOpenNavigation && (
                <button
                    className="icon-button workspace-navigation-trigger"
                    type="button"
                    title="打开项目导航"
                    aria-label="打开项目导航"
                    aria-haspopup="dialog"
                    aria-expanded={navigationOpen}
                    aria-controls={navigationOpen ? 'workspace-navigation' : undefined}
                    onClick={onOpenNavigation}
                >
                    <Menu size={18} strokeWidth={1.8} aria-hidden="true" />
                </button>
            )}
            <div className="brand-mark mark" aria-label="Praxis">P</div>
            <b>Praxis</b>
            <div className="crumb">
                {projectName || '代理工作区'}
                {sessionTitle && <span> / {sessionTitle}</span>}
            </div>
            <div className="spacer" />
            <div className="status">
                <i className={`dot ${busy || refreshing ? 'warn' : ''}`} aria-hidden="true" />
                {refreshing ? '刷新中' : busy ? 'Primary 执行中' : '已就绪'}
            </div>
            <button
                className="quiet-button ghost"
                type="button"
                aria-label={refreshing ? '刷新中' : '刷新工作区'}
                title={refreshing ? '刷新中' : '刷新工作区'}
                onClick={onRefresh}
                disabled={busy || refreshing}
            >
                <RefreshCw size={14} strokeWidth={1.8} aria-hidden="true" />
                <span className="quiet-button-label">{refreshing ? '刷新中...' : '刷新'}</span>
            </button>
        </header>
    )
}
