import { FolderPlus, GitBranch, ListOrdered, ShieldCheck, Terminal } from 'lucide-react'

type SessionEmptyStateProps = {
    sessionsCount: number
    projectsCount?: number
    onOpenProject?: () => void
}

export function SessionEmptyState({ sessionsCount, projectsCount = 0, onOpenProject }: SessionEmptyStateProps) {
    const hasExistingSessions = sessionsCount > 0
    const hasProjects = projectsCount > 0

    return (
        <div className="session-empty-container">
            <div className="session-empty-hero">
                <div className="session-empty-icon" aria-hidden="true">
                    <Terminal size={24} strokeWidth={1.7} />
                </div>
                <span className="empty-kicker">PRAXIS · 本地多 AGENT 工作台</span>
                <h1 className="session-empty-title">
                    {!hasProjects ? '绑定项目目录，开始编排任务' : hasExistingSessions ? '选择或新建会话' : '在项目中新建会话'}
                </h1>
                <p className="session-empty-description">
                    以本地项目目录为执行边界，由主代理持续推进任务、受控调用工具，并委派上下文隔离的子代理协同完成工作。
                </p>
            </div>

            <div className="session-empty-features">
                <div className="session-empty-feature">
                    <div className="session-empty-feature-icon" aria-hidden="true">
                        <GitBranch size={18} strokeWidth={1.8} />
                    </div>
                    <div className="session-empty-feature-content">
                        <h2 className="session-empty-feature-title">主从代理分工</h2>
                        <p className="session-empty-feature-text">
                            主代理拆解任务并委派子代理并行处理，子代理上下文独立并在完成后交付汇报。
                        </p>
                    </div>
                </div>

                <div className="session-empty-feature">
                    <div className="session-empty-feature-icon" aria-hidden="true">
                        <ShieldCheck size={18} strokeWidth={1.8} />
                    </div>
                    <div className="session-empty-feature-content">
                        <h2 className="session-empty-feature-title">授权与审批闸口</h2>
                        <p className="session-empty-feature-text">
                            文件补丁与终端命令按 ask / auto / yolo 策略执行，高风险操作固定在输入区上方确认。
                        </p>
                    </div>
                </div>

                <div className="session-empty-feature">
                    <div className="session-empty-feature-icon" aria-hidden="true">
                        <ListOrdered size={18} strokeWidth={1.8} />
                    </div>
                    <div className="session-empty-feature-content">
                        <h2 className="session-empty-feature-title">队列与连续日志</h2>
                        <p className="session-empty-feature-text">
                            执行中可按 FIFO 追加后续输入，历史、耗时与 Token 用量按条目落盘，支持 /reload 热重载。
                        </p>
                    </div>
                </div>
            </div>

            <div className="session-empty-action">
                {!hasProjects && onOpenProject ? (
                    <button className="session-empty-cta" type="button" onClick={onOpenProject}>
                        <FolderPlus size={16} strokeWidth={1.8} aria-hidden="true" />
                        <span>新建项目</span>
                    </button>
                ) : (
                    <div className="session-empty-hint">
                        <span>点左侧项目图标或按</span>
                        <kbd className="session-empty-kbd">Ctrl+K</kbd>
                        <span>{hasExistingSessions ? '选择或新建会话' : '在项目中新建会话'}</span>
                        <span className="session-empty-sep" aria-hidden="true">·</span>
                        <span>会话内输入</span>
                        <kbd className="session-empty-kbd">/</kbd>
                        <span>可执行 /new 与 /reload</span>
                    </div>
                )}
            </div>
        </div>
    )
}
