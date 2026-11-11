import { Bot, Check, CircleAlert, Wrench } from 'lucide-react'
import type { AgentsPanelProps } from './types'

const stateLabels: Record<string, string> = {
    idle: '空闲', executing: '执行中', waiting: '等待中',
    interrupted: '已中断', failed: '失败', unknown: '未知',
}

export function AgentsPanel({ loading, session, selectedAgentID, onSelectAgent }: AgentsPanelProps) {
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
                                const run = item.currentRun || item.latestRun
                                const state = stateLabels[item.state] || stateLabels.unknown
                                return (
                                    <button
                                        className={`agent-row agent ${item.ID === selectedAgentID ? 'selected' : ''}`}
                                        key={item.ID}
                                        type="button"
                                        onClick={() => onSelectAgent(item.ID)}
                                        aria-current={item.ID === selectedAgentID ? 'true' : undefined}
                                    >
                                        <span className={`state-marker astate state-${item.state}`} aria-hidden="true" />
                                        <span className="agent-row-copy">
                                            <strong className="nm">{item.name}</strong>
                                            <span className="agent-row-metrics">
                                                <span title="最近执行的模型迭代数"><Check size={11} aria-hidden="true" />{run?.Iterations ?? 0}</span>
                                                <span title="最近执行的工具调用数"><Wrench size={11} aria-hidden="true" />{run?.ToolCalls ?? 0}</span>
                                                {run?.Outcome === 'failed' && <span className="agent-metric-danger" title="最近执行失败"><CircleAlert size={11} aria-hidden="true" /></span>}
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
