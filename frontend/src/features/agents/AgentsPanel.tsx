import { Bot } from 'lucide-react'
import { AgentsPanelProps } from './types'

export function AgentsPanel({ loading, session, selectedAgentID, onSelectAgent }: AgentsPanelProps) {
    return (
        <aside className="sidebar agents-panel">
            <div className="sidebar-heading">
                <span className="section-label">会话代理</span>
                <span className="sidebar-count">{session?.agents.length || 0}</span>
            </div>
            {session ? (
                <>
                    <div className="session-meta">
                        <span className="session-title">{session.title || '未命名会话'}</span>
                        <span>{session.agents.length} 个代理在此会话中</span>
                    </div>
                    <div className="scroll-region">
                        <div className="agent-list">
                            {session.agents.map((item) => (
                                <button
                                    className={`agent-row ${item.id === selectedAgentID ? 'selected' : ''}`}
                                    key={item.id}
                                    type="button"
                                    onClick={() => onSelectAgent(item.id)}
                                >
                                    <span className={`state-marker state-${item.state}`} />
                                    <span className="agent-row-copy">
                                        <strong>{item.definitionId}</strong>
                                        <small>{item.id}</small>
                                    </span>
                                    <span className="state-text">{item.state}</span>
                                </button>
                            ))}
                        </div>
                    </div>
                </>
            ) : loading ? null : (
                <div className="panel-placeholder">
                    <Bot size={18} strokeWidth={1.7} aria-hidden="true" />
                    <p>选择一个会话以查看其代理。</p>
                </div>
            )}
        </aside>
    )
}
