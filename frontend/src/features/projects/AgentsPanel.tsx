import {Bot} from 'lucide-react'
import {AgentsPanelProps} from './types'

export function AgentsPanel({loading, session, selectedAgentID, onSelectAgent}: AgentsPanelProps) {
    return (
        <aside className="sidebar agents-panel">
            <div className="sidebar-heading">
                <span className="section-label">Session agents</span>
                <span className="sidebar-count">{session?.agents.length || 0}</span>
            </div>
            {session ? (
                <>
                    <div className="session-meta">
                        <span className="session-goal">{session.goal}</span>
                        <span>{session.agents.length} agent{session.agents.length === 1 ? '' : 's'} in this session</span>
                    </div>
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
                                    <strong>{item.profile}</strong>
                                    <small>{item.id}</small>
                                </span>
                                <span className="state-text">{item.state}</span>
                            </button>
                        ))}
                    </div>
                </>
            ) : loading ? null : (
                <div className="panel-placeholder">
                    <Bot size={18} strokeWidth={1.7} aria-hidden="true" />
                    <p>Select a session to see its agents.</p>
                </div>
            )}
        </aside>
    )
}
