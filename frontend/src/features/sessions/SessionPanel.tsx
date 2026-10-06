import type { ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import { SessionEmptyState } from './SessionEmptyState'

type SessionPanelProps = {
    loading: boolean
    hasSession: boolean
    sessionsCount: number
    children?: ReactNode
}

/** 管理 Session 页面外壳、加载态与空状态。 */
export function SessionPanel({ loading, hasSession, sessionsCount, children }: SessionPanelProps) {
    if (!hasSession) {
        if (loading) {
            return <section className="session-loading" role="status" aria-busy="true"><LoaderCircle size={18} className="is-spinning" aria-hidden="true" /><span>正在加载会话</span></section>
        }
        return (
            <div className="scroll-region">
                <section className="page session-empty-state">
                    <SessionEmptyState sessionsCount={sessionsCount} />
                </section>
            </div>
        )
    }

    return (
        <section className="session-panel">
            {children}
        </section>
    )
}
