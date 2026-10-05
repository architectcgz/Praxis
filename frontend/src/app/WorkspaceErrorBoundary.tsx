import { Component, type ErrorInfo, type ReactNode } from 'react'
import { RefreshCw, TriangleAlert } from 'lucide-react'
import './error-boundary.css'

type WorkspaceErrorBoundaryProps = {
    children: ReactNode
}

type WorkspaceErrorBoundaryState = {
    failed: boolean
}

// 当边界契约发生回归时，统一提供可恢复的错误界面。
export class WorkspaceErrorBoundary extends Component<WorkspaceErrorBoundaryProps, WorkspaceErrorBoundaryState> {
    state: WorkspaceErrorBoundaryState = { failed: false }

    static getDerivedStateFromError(): WorkspaceErrorBoundaryState {
        return { failed: true }
    }

    componentDidCatch(error: Error, info: ErrorInfo) {
        console.error('工作区渲染失败。', error, info)
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
                    <h1>工作区暂不可用</h1>
                    <p>Praxis 无法渲染工作区，请检查配置后重新加载。</p>
                </div>
                <button
                    className="icon-button app-error-reload"
                    type="button"
                    title="重新加载工作区"
                    aria-label="重新加载工作区"
                    onClick={() => window.location.reload()}
                >
                    <RefreshCw size={17} strokeWidth={1.9} aria-hidden="true" />
                </button>
            </main>
        )
    }
}
