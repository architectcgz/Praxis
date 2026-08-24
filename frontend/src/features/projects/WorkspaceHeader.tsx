type WorkspaceHeaderProps = {
    bridgeState: string
    ready: boolean
	issue: boolean
    busy: boolean
    refreshing: boolean
    onRefresh: () => void
}

export function WorkspaceHeader({bridgeState, ready, issue, busy, refreshing, onRefresh}: WorkspaceHeaderProps) {
    return (
        <header className="topbar">
            <div className="brand-mark" aria-label="Praxis">P</div>
            <div>
                <strong>Praxis</strong>
                <span>Agent workspace</span>
            </div>
			<div className={`health health-${issue ? 'error' : ready ? 'ready' : 'waiting'}`}>
                <span className="health-dot" aria-hidden="true" />
                {bridgeState}
            </div>
            <button
                className="quiet-button"
                type="button"
                onClick={onRefresh}
                disabled={busy || refreshing}
            >
                {refreshing ? 'Refreshing...' : 'Refresh'}
            </button>
        </header>
    )
}
