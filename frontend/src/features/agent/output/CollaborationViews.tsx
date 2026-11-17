import { Bot, ChevronDown, ExternalLink, GitBranch } from 'lucide-react'
import type { Entry, AgentView } from '../../../api'
import { MessageMarkdown } from './MarkdownViews'
import { OutputStatus } from './OutputStatus'
import type { ToolStatus } from './toolPresentation'
import type { StreamingOutput } from '../types'
import { RunDuration } from '../../timing/DurationLabel'

type CollaborationViewsProps = {
    agents: AgentView[]
    agentEntries: Record<string, Entry[]>
    entriesErrors: Record<string, string>
    streamingOutputs: Record<string, StreamingOutput>
    onSelectAgent: (id: string) => void
}

/** 统一展示当前会话委派的 Subagent 并行泳道与交接结果，避免同一批子代理在两张卡片里重复出现。 */
export function CollaborationViews({ agents, agentEntries, entriesErrors, streamingOutputs, onSelectAgent }: CollaborationViewsProps) {
    const subagents = agents.filter((agent) => agent.Kind === 'subagent')
    if (subagents.length === 0) return null
    const handoffs = subagents.map((agent) => subagentHandoffView(agent, agentEntries[agent.ID], streamingOutputs[agent.ID], entriesErrors[agent.ID]))
    const completed = handoffs.filter((item) => item.status === 'completed').length
    const running = handoffs.filter((item) => item.status === 'running').length
    const blocked = handoffs.filter((item) => item.status === 'failed' || item.status === 'blocked' || item.status === 'cancelled').length
    const isParallel = handoffs.length > 1
    return (
        <div className="message collaboration-messages">
            <section className="output-card output-handoff" aria-label={isParallel ? '多代理协作' : '委派与交接'}>
                <div className="output-card-head">
                    {isParallel ? <GitBranch size={15} aria-hidden="true" /> : <Bot size={15} aria-hidden="true" />}
                    <h3>{isParallel ? `${handoffs.length} 个子代理协作` : '委派与交接'}</h3>
                    <span className="output-muted">
                        {isParallel
                            ? `${completed} / ${handoffs.length} 已完成${running > 0 ? ` · ${running} 执行中` : ''}${blocked > 0 ? ` · ${blocked} 待处理` : ''}`
                            : handoffs[0].label}
                    </span>
                </div>
                <div className="output-card-body">
                    {isParallel && (
                        <div className="output-lanes">
                            {handoffs.map((item) => (
                                <article className="output-lane" key={item.agent.ID}>
                                    <div className="output-lane-head">
                                        <strong>{item.agent.name}</strong>
                                        <OutputStatus status={item.status} label={item.label} />
                                    </div>
                                    <RunDuration run={item.run} label="耗时" />
                                    <p className="output-lane-task">{item.task || '尚未提供任务输入'}</p>
                                    <div className="output-lane-track" data-status={item.status} aria-hidden="true"><span /></div>
                                    <p className="output-lane-result">{item.error || item.result || '尚未返回结果'}</p>
                                    <button className="output-button" type="button" onClick={() => onSelectAgent(item.agent.ID)}>
                                        <ExternalLink size={12} aria-hidden="true" />查看详情
                                    </button>
                                </article>
                            ))}
                        </div>
                    )}
                    {handoffs.map((item) => (
                        <details className="output-fold output-handoff-task" key={item.agent.ID} open={!isParallel && handoffs.length === 1 && item.status === 'running' ? true : undefined}>
                            <summary className="output-fold-button">
                                <strong>{item.agent.name}</strong>
                                <OutputStatus status={item.status} label={item.label} />
                                <RunDuration run={item.run} label="耗时" />
                                <ChevronDown className="output-chevron" size={14} aria-hidden="true" />
                            </summary>
                            <div className="output-fold-body">
                                <dl className="output-handoff-meta">
                                    <dt>任务输入</dt><dd>{item.task || '尚未提供任务输入'}</dd>
                                    <dt>执行编号</dt><dd><code>{item.runId || '尚未开始执行'}</code></dd>
                                    <dt>当前状态</dt><dd>{item.label}{item.error && ` · ${item.error}`}</dd>
                                </dl>
                                {item.result ? (
                                    <div className="output-handoff-result">
                                        <span className="output-muted">{item.status === 'completed' ? '交接结果' : '当前输出'}</span>
                                        <MessageMarkdown content={item.result} />
                                    </div>
                                ) : <p className="output-muted">尚未返回交接结果。</p>}
                                <div>
                                    <button className="output-button" type="button" onClick={() => onSelectAgent(item.agent.ID)}>
                                        <ExternalLink size={12} aria-hidden="true" />查看完整历史
                                    </button>
                                </div>
                            </div>
                        </details>
                    ))}
                </div>
            </section>
        </div>
    )
}

function subagentHandoffView(agent: AgentView, entries?: Entry[], streaming?: StreamingOutput, entriesError?: string) {
    const run = agent.currentRun || agent.latestRun
    const live = streaming?.runId === agent.currentRunId ? streaming : undefined
    const runId = live?.runId || run?.ID || ''
    const messages = (entries || []).filter((item) => !runId || item.runId === runId)
    const text = (item?: Entry) => item?.content.filter((block) => block.kind === 'text').map((block) => block.text || '').join('') || ''
    const input = messages.find((item) => item.role === 'user')
    const reply = messages.findLast((item) => item.role === 'assistant' && text(item))
    const result = live?.turns
        .map((turn) => turn.events.filter((event) => event.kind === 'text_delta').map((event) => event.data.text).join('').trim())
        .filter(Boolean)
        .join('\n\n') || text(reply)
    let status: ToolStatus = 'queued'
    let label = '等待执行'
    if (live?.error) {
        status = 'failed'
        label = '失败'
    } else if (agent.state === 'interrupted') {
        status = 'cancelled'
        label = '已中断'
    } else if (agent.state === 'executing' || live) {
        status = 'running'
        label = '进行中'
    } else if (agent.state === 'failed' || run?.Outcome === 'failed') {
        status = 'failed'
        label = '失败'
    } else if (run?.Outcome === 'interrupted') {
        status = 'cancelled'
        label = '已中断'
    } else if (agent.state === 'waiting') {
        status = 'blocked'
        label = '等待输入'
    } else if (run?.Status === 'ended' && (run.Outcome === 'completed' || run.Outcome === 'yielded')) {
        status = 'completed'
        label = '已完成'
    }
    return { agent, runId, run, task: entries ? text(input) : '选择该 Agent 后加载任务历史', result, status, label, error: entriesError ? `条目加载失败：${entriesError}` : live?.error || run?.FailureCode || '' }
}
