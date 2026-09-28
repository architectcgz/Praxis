type WorkspaceHeaderProps = {
    busy: boolean
    refreshing: boolean
    onRefresh: () => void
}

export function WorkspaceHeader({ busy, refreshing, onRefresh }: WorkspaceHeaderProps) {
    return (
        <header className="topbar">
            <div className="brand-mark" aria-label="Praxis">P</div>
            <div>
                <strong>Praxis</strong>
                <span>代理工作区</span>
            </div>
            <button
                className="quiet-button"
                type="button"
                onClick={onRefresh}
                disabled={busy || refreshing}
            >
                {refreshing ? '刷新中...' : '刷新'}
            </button>
        </header>
    )
}
