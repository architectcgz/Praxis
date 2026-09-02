import { FolderPlus, MessageSquare, Sparkles, Zap } from 'lucide-react'

type SessionEmptyStateProps = {
    sessionsCount: number
}

export function SessionEmptyState({ sessionsCount }: SessionEmptyStateProps) {
    const hasExistingSessions = sessionsCount > 0

    return (
        <div className="session-empty-container">
            <div className="session-empty-hero">
                <div className="session-empty-icon-wrapper">
                    <div className="session-empty-icon-background" />
                    <div className="session-empty-icon-main">
                        <MessageSquare size={32} strokeWidth={1.6} />
                    </div>
                    <div className="session-empty-icon-accent">
                        <Sparkles size={18} strokeWidth={2} />
                    </div>
                </div>

                <h2 className="session-empty-title">
                    {hasExistingSessions ? '选择一个会话开始工作' : '开始您的第一次协作'}
                </h2>

                <p className="session-empty-description">
                    {hasExistingSessions
                        ? '从左侧面板中选择一个会话，查看对话历史并继续与 AI 代理协作。'
                        : '会话是您与 AI 代理协作的工作空间。在会话中，您可以与代理对话、执行任务、管理项目进度。'}
                </p>
            </div>

            <div className="session-empty-features">
                <div className="session-empty-feature">
                    <div className="session-empty-feature-icon">
                        <MessageSquare size={20} strokeWidth={1.8} />
                    </div>
                    <div className="session-empty-feature-content">
                        <h3 className="session-empty-feature-title">智能对话</h3>
                        <p className="session-empty-feature-text">
                            与 AI 代理进行自然对话，获得即时帮助和建议
                        </p>
                    </div>
                </div>

                <div className="session-empty-feature">
                    <div className="session-empty-feature-icon">
                        <Zap size={20} strokeWidth={1.8} />
                    </div>
                    <div className="session-empty-feature-content">
                        <h3 className="session-empty-feature-title">任务执行</h3>
                        <p className="session-empty-feature-text">
                            自动执行复杂任务，提高工作效率
                        </p>
                    </div>
                </div>

                <div className="session-empty-feature">
                    <div className="session-empty-feature-icon">
                        <FolderPlus size={20} strokeWidth={1.8} />
                    </div>
                    <div className="session-empty-feature-content">
                        <h3 className="session-empty-feature-title">项目管理</h3>
                        <p className="session-empty-feature-text">
                            组织和跟踪多个项目的工作进展
                        </p>
                    </div>
                </div>
            </div>

            <div className="session-empty-action">
                {hasExistingSessions ? (
                    <div className="session-empty-hint">
                        <kbd className="session-empty-kbd">←</kbd>
                        <span>从左侧选择会话</span>
                    </div>
                ) : (
                    <div className="session-empty-hint">
                        <span>选择一个项目，然后点击</span>
                        <kbd className="session-empty-kbd">新建会话</kbd>
                        <span>开始</span>
                    </div>
                )}
            </div>
        </div>
    )
}
