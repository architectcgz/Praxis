import { CalendarClock, FolderOpen, MessageSquare, MessageSquarePlus } from 'lucide-react'
import { ProjectGroup } from './types'

type ProjectOverviewProps = {
    project: ProjectGroup
    selectedSessionID: string
    onSelectSession: (id: string) => void
    onOpenSession: (project: ProjectGroup) => void
}

export function ProjectOverview({ project, selectedSessionID, onSelectSession, onOpenSession }: ProjectOverviewProps) {
    const latestSession = project.sessions[0]
    return (
        <div className="scroll-region">
            <section className="page project-overview" aria-labelledby="project-overview-title">
                <header className="project-overview-header">
                    <div className="project-overview-title-row">
                        <div className="project-overview-icon" aria-hidden="true"><FolderOpen size={24} strokeWidth={1.8} /></div>
                        <div>
                            <span className="section-label">项目</span>
                            <h1 id="project-overview-title">{project.name}</h1>
                        </div>
                    </div>
                    <button className="quiet-button project-overview-action" type="button" onClick={() => onOpenSession(project)}>
                        <MessageSquarePlus size={16} strokeWidth={1.8} aria-hidden="true" />
                        <span>新建会话</span>
                    </button>
                </header>

                <div className="project-overview-path"><FolderOpen size={15} strokeWidth={1.8} aria-hidden="true" /><span>{project.path}</span></div>

                <div className="project-overview-stats" aria-label="项目统计">
                    <div><strong>{project.sessions.length}</strong><span>个会话</span></div>
                    <div><strong>{latestSession ? formatDate(latestSession.updatedAt) : '暂无'}</strong><span>最近活动</span></div>
                </div>

                <section className="project-overview-section" aria-labelledby="project-sessions-title">
                    <div className="project-overview-section-heading">
                        <div><MessageSquare size={17} strokeWidth={1.8} aria-hidden="true" /><h2 id="project-sessions-title">会话</h2></div>
                        <span>{project.sessions.length}</span>
                    </div>
                    {project.sessions.length ? (
                        <div className="project-overview-sessions">
                            {project.sessions.map((session) => (
                                <button className={`project-overview-session ${session.id === selectedSessionID ? 'selected' : ''}`} key={session.id} type="button" onClick={() => onSelectSession(session.id)}>
                                    <span className="project-overview-session-copy"><strong>{session.goal || '未命名会话'}</strong><small>{formatDate(session.updatedAt)}</small></span>
                                    <CalendarClock size={16} strokeWidth={1.8} aria-hidden="true" />
                                </button>
                            ))}
                        </div>
                    ) : (
                        <p className="muted-copy">这个项目还没有会话。</p>
                    )}
                </section>
            </section>
        </div>
    )
}

function formatDate(value: string) {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) {
        return '暂无'
    }
    return new Intl.DateTimeFormat('zh-CN', { year: 'numeric', month: 'short', day: 'numeric' }).format(date)
}
