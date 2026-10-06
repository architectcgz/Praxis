import { Bot, ChevronDown, ExternalLink, GitBranch } from 'lucide-react'
import type { AgentHistoryItem, AgentSnapshot } from '../../../api'
import { MessageMarkdown } from './MarkdownViews'
import { OutputStatus } from './OutputStatus'
import type { ToolStatus } from './toolPresentation'
import type { StreamingOutput } from '../types'
import { OperationDuration } from '../../timing/OperationTimings'

type CollaborationViewsProps = {
    agents: AgentSnapshot[]
    histories: Record<string, AgentHistoryItem[]>
    historyErrors: Record<string, string>
    streamingOutputs: Record<string, StreamingOutput>
    onSelectAgent: (id: string) => void
}

/** 展示同一会话的委派任务和并行分支；只以实际执行结算判断完成，不推测百分比。 */
export function CollaborationViews({ agents, histories, historyErrors, streamingOutputs, onSelectAgent }: CollaborationViewsProps) {
    const delegates = agents.filter((agent) => agent.definitionId === 'delegate' || agent.profile === 'delegate')
    if (delegates.length === 0) return null
    const tasks = delegates.map((agent) => taskView(agent, histories[agent.id] || [], streamingOutputs[agent.id], historyErrors[agent.id]))
    const completed = tasks.filter((task) => task.status === 'completed').length
    const running = tasks.filter((task) => task.status === 'running').length
    const blocked = tasks.filter((task) => task.status === 'failed' || task.status === 'blocked' || task.status === 'cancelled').length
    return <div className="message collaboration-messages">
        {tasks.length > 1 && <section className="output-card output-parallel" aria-label="并行任务">
            <div className="output-card-head"><GitBranch size={15} aria-hidden="true" /><h3>{tasks.length} 个 Agent 并行协作</h3><span className="output-muted">{completed} 个任务已完成</span></div>
            <div className="output-card-body">
                <div className="output-lanes">
                    {tasks.map((task) => <article className="output-lane" key={task.agent.id}>
                        <div className="output-lane-head"><strong>{task.agent.name}</strong><OutputStatus status={task.status} label={task.label} /></div>
                        <OperationDuration taskId={task.taskId} kind="agent" label="子 Agent 耗时" />
                        <p className="output-lane-task">{task.task || '尚未提供任务输入'}</p>
                        <div className="output-lane-track" data-status={task.status} aria-hidden="true"><span /></div>
                        <p className="output-lane-result">{task.error || task.result || '尚未返回结果'}</p>
                        <button className="output-button" type="button" onClick={() => onSelectAgent(task.agent.id)}><ExternalLink size={12} aria-hidden="true" />查看 Agent</button>
                    </article>)}
                </div>
                <p className="output-muted">{completed} / {tasks.length} 已完成 · {running} 执行中{blocked > 0 && ` · ${blocked} 个分支待处理`}</p>
            </div>
        </section>}
        <section className="output-card output-handoff" aria-label="委派与交接">
            <div className="output-card-head"><Bot size={15} aria-hidden="true" /><h3>委派与交接</h3><span className="output-muted">{tasks.length} 个任务负责人</span></div>
            <div className="output-card-body">
                {tasks.map((task) => <details className="output-fold output-handoff-task" key={task.agent.id}>
                    <summary className="output-fold-button"><strong>{task.agent.name}</strong><OutputStatus status={task.status} label={task.label} /><OperationDuration taskId={task.taskId} kind="agent" label="子 Agent 耗时" /><ChevronDown className="output-chevron" size={14} aria-hidden="true" /></summary>
                    <div className="output-fold-body">
                        <dl className="output-handoff-meta"><dt>任务输入</dt><dd>{task.task || '尚未提供任务输入'}</dd><dt>执行编号</dt><dd><code>{task.taskId || '尚未开始执行'}</code></dd><dt>当前状态</dt><dd>{task.label}{task.error && ` · ${task.error}`}</dd></dl>
                        {task.result ? <div className="output-handoff-result"><span className="output-muted">{task.status === 'completed' ? '交接结果' : '当前输出'}</span><MessageMarkdown content={task.result} /></div> : <p className="output-muted">尚未返回交接结果。</p>}
                        <div><button className="output-button" type="button" onClick={() => onSelectAgent(task.agent.id)}><ExternalLink size={12} aria-hidden="true" />查看完整任务</button></div>
                    </div>
                </details>)}
            </div>
        </section>
    </div>
}

function taskView(agent: AgentSnapshot, history: AgentHistoryItem[], streaming?: StreamingOutput, historyError?: string) {
    const task = agent.tasks.find((item) => item.id === agent.currentTaskId) || [...agent.tasks].sort((left, right) => Date.parse(right.createdAt) - Date.parse(left.createdAt))[0]
    const live = streaming && !agent.tasks.some((item) => item.id === streaming.taskId && item.status === 'ended') ? streaming : undefined
    const taskId = live?.taskId || task?.id || ''
    const messages = history.filter((item) => item.message && (!taskId || item.message.taskId === taskId)).sort((left, right) => (right.message?.sequence || 0) - (left.message?.sequence || 0))
    const input = messages.find((item) => item.message?.role === 'user')?.message
    const reply = messages.find((item) => item.message?.role === 'assistant' && (item.message.content || item.message.blocks?.some((block) => block.kind === 'text' && block.text)))?.message
    const result = live?.turns.flatMap((turn) => turn.events.filter((event) => event.kind === 'text_delta').map((event) => event.text || '')).join('') || reply?.content || reply?.blocks?.filter((block) => block.kind === 'text').map((block) => block.text || '').join('\n') || ''
    let status: ToolStatus = 'queued'
    let label = '等待执行'
    if (live?.error) {
        status = 'failed'
        label = '失败'
    } else if (agent.state === 'executing' || agent.state === 'pausing' || live) {
        status = 'running'
        label = agent.state === 'pausing' ? '暂停中' : '进行中'
    } else if (agent.state === 'failed' || task?.outcome === 'failed') {
        status = 'failed'
        label = '失败'
    } else if (agent.state === 'paused' || agent.state === 'interrupted' || task?.outcome === 'paused' || task?.outcome === 'interrupted') {
        status = 'cancelled'
        label = agent.state === 'paused' || task?.outcome === 'paused' ? '已暂停' : '已中断'
    } else if (agent.state === 'waiting' || task?.outcome === 'yielded') {
        status = 'blocked'
        label = '等待输入'
    } else if (task?.status === 'ended' && task.outcome === 'completed') {
        status = 'completed'
        label = '已完成'
    }
    return { agent, taskId, task: input?.content || input?.blocks?.filter((block) => block.kind === 'text').map((block) => block.text || '').join('\n') || '', result, status, label, error: historyError ? `历史加载失败：${historyError}` : live?.error || task?.failureCode || '' }
}
