import { Component, type ErrorInfo, type ReactNode } from 'react'
import { RefreshCw, TriangleAlert } from 'lucide-react'

type WorkspaceErrorBoundaryProps = {
    children: ReactNode
}

type WorkspaceErrorBoundaryState = {
    failed: boolean
}

// This is the final UI safety net when a boundary contract regresses.
export class WorkspaceErrorBoundary extends Component<WorkspaceErrorBoundaryProps, WorkspaceErrorBoundaryState> {
    state: WorkspaceErrorBoundaryState = { failed: false }

    static getDerivedStateFromError(): WorkspaceErrorBoundaryState {
        return { failed: true }
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        console.error('Workspace render failed.', error, info)
    }

    render() {
        if (!this.state.failed) {
            return this.props.children
        }
        return (
            <main className="app-error-state" role="alert">
                <span className="app-error-icon" aria-hidden="true">
                    <TriangleAlert size={22} strokeWidth={1.8} />
                </span>
                <div>
                    <h1>Workspace unavailable</h1>
                    <p>Praxis could not render the workspace. Check the configuration and reload.</p>
                </div>
                <button
                    className="icon-button app-error-reload"
                    type="button"
                    title="Reload workspace"
                    aria-label="Reload workspace"
                    onClick={() => window.location.reload()}
                >
                    <RefreshCw size={17} strokeWidth={1.9} aria-hidden="true" />
                </button>
            </main>
        )
    }
}
