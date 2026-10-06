import { Bot, Check, CircleAlert, Wrench } from 'lucide-react'
import type { AgentHistoryItem } from '../../api'
import type { AgentsPanelProps } from './types'

const stateLabels: Record<string, string> = {
    idle: '空闲', executing: '执行中', waiting: '等待中', pausing: '暂停中', paused: '已暂停',
    interrupted: '已中断', failed: '失败', unknown: '未知',
}

export function AgentsPanel({ loading, session, histories, selectedAgentID, onSelectAgent }: AgentsPanelProps) {
    const agents = session?.agents || []
    return (
        <aside className="sidebar side right agents-panel">
            <div className="sidebar-heading side-head">
                <span className="section-label">会话代理</span>
                <span className="sidebar-count">{agents.length}</span>
            </div>
            {session ? (
                <>
                    <div className="session-meta session-note">
                        <b className="session-title">{session.title || '未命名会话'}</b>
                        <span>{agents.length} 个代理协作中</span>
                    </div>
                    <div className="scroll-region scroll">
                        <div className="agent-list">
                            {agents.map((item) => {
                                const history = histories[item.id] || []
                                const stats = agentStats(history)
                                const state = stateLabels[item.state] || stateLabels.unknown
                                return (
                                    <button
                                        className={`agent-row agent ${item.id === selectedAgentID ? 'selected' : ''}`}
                                        key={item.id}
                                        type="button"
                                        onClick={() => onSelectAgent(item.id)}
                                        aria-current={item.id === selectedAgentID ? 'true' : undefined}
                                    >
                                        <span className={`state-marker astate state-${item.state}`} aria-hidden="true" />
                                        <span className="agent-row-copy">
                                            <strong className="nm">{item.name}</strong>
                                            <span className="agent-row-metrics">
                                                <span><Check size={11} aria-hidden="true" />{stats.messages}</span>
                                                <span><Wrench size={11} aria-hidden="true" />{stats.tools}</span>
                                                {stats.failures > 0 && <span className="agent-metric-danger"><CircleAlert size={11} aria-hidden="true" />{stats.failures}</span>}
                                            </span>
                                        </span>
                                        <span className="state-text st">{state}</span>
                                    </button>
                                )
                            })}
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

function agentStats(history: AgentHistoryItem[]) {
    let messages = 0
    let tools = 0
    let failures = 0
    for (const item of history) {
        if (item.message) {
            messages++
            for (const block of item.message.blocks || []) {
                if (block.kind === 'tool_call') tools++
                if (block.kind === 'tool_result' && block.isError) failures++
            }
        }
        if (item.turn?.outcome === 'failed') failures++
    }
    return { messages, tools, failures }
}
