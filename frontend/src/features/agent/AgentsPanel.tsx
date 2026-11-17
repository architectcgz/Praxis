import { Bot, Check, CircleAlert, Wrench } from 'lucide-react'
import type { AgentView } from '../../api'
import type { AgentsPanelProps } from './types'

const stateLabels: Record<string, string> = {
    idle: '空闲', executing: '执行中', waiting: '等待中',
    interrupted: '已中断', failed: '失败', unknown: '未知',
}

function agentStatusLabel(agent: AgentView): string {
    if (agent.state !== 'idle') return stateLabels[agent.state] || stateLabels.unknown
    if (agent.Kind === 'subagent' && agent.latestRun?.Status === 'ended') {
        if (agent.latestRun.Outcome === 'yielded') return '已交付'
        if (agent.latestRun.Outcome === 'completed') return '已完成'
        if (agent.latestRun.Outcome === 'failed') return '失败'
        if (agent.latestRun.Outcome === 'interrupted') return '已中断'
    }
    return stateLabels.idle
}

function agentToneState(agent: AgentView): string {
    if (agent.state !== 'idle') return agent.state
    if (agent.Kind === 'subagent' && agent.latestRun?.Outcome === 'failed') return 'failed'
    if (agent.Kind === 'subagent' && agent.latestRun?.Outcome === 'interrupted') return 'interrupted'
    return 'idle'
}

export function AgentsPanel({ loading, session, selectedAgentID, onSelectAgent }: AgentsPanelProps) {
    const allAgents = session?.agents || []
    const mainAgents = allAgents.filter((agent) => agent.Kind === 'main')
    const subagents = allAgents
        .filter((agent) => agent.Kind === 'subagent')
        .sort((left, right) => {
            const leftActive = left.currentRunId ? 1 : 0
            const rightActive = right.currentRunId ? 1 : 0
            if (leftActive !== rightActive) return rightActive - leftActive
            return right.UpdatedAt.localeCompare(left.UpdatedAt)
        })
    const activeCount = allAgents.filter((agent) => agent.state === 'executing' || agent.state === 'waiting').length

    const renderAgentRow = (item: AgentView) => {
        const run = item.currentRun || item.latestRun
        const state = agentStatusLabel(item)
        const tone = agentToneState(item)
        return (
            <button
                className={`agent-row agent ${item.ID === selectedAgentID ? 'selected' : ''}`}
                key={item.ID}
                type="button"
                onClick={() => onSelectAgent(item.ID)}
                aria-current={item.ID === selectedAgentID ? 'true' : undefined}
            >
                <span className={`state-marker astate state-${tone}`} aria-hidden="true" />
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
    }

    return (
        <aside className="sidebar side right agents-panel">
            <div className="sidebar-heading side-head">
                <span className="section-label">会话代理</span>
                <span className="sidebar-count">{allAgents.length}</span>
            </div>
            {session ? (
                <>
                    <div className="session-meta session-note">
                        <b className="session-title">{session.title || '未命名会话'}</b>
                        <span>{activeCount > 0 ? `${activeCount} 个运行中 · 共 ${allAgents.length} 个代理` : `全部空闲 · 共 ${allAgents.length} 个代理`}</span>
                    </div>
                    <div className="scroll-region scroll">
                        {subagents.length > 0 ? (
                            <div className="agent-groups">
                                {mainAgents.length > 0 && (
                                    <div className="agent-group">
                                        <span className="agent-group-label">主代理</span>
                                        <div className="agent-list">{mainAgents.map(renderAgentRow)}</div>
                                    </div>
                                )}
                                <div className="agent-group">
                                    <span className="agent-group-label">子代理 ({subagents.length})</span>
                                    <div className="agent-list">{subagents.map(renderAgentRow)}</div>
                                </div>
                            </div>
                        ) : (
                            <div className="agent-list">{mainAgents.map(renderAgentRow)}</div>
                        )}
                    </div>
                </>
            ) : loading ? null : (
                <div className="panel-placeholder">
                    <Bot size={18} strokeWidth={1.7} aria-hidden="true" />
                    <p>选择一个会话以查看主代理与委派子代理。</p>
                </div>
            )}
        </aside>
    )
}
