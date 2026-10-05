import { ChevronDown } from 'lucide-react'
import type { AgentEvent, AgentMessageBlock } from '../../../api'
import { FileChanges } from './FileChanges'
import { OutputStatus } from './OutputStatus'
import { TerminalOutput } from './TerminalOutput'
import { ToolResultContent } from './ToolResultContent'
import { formatToolInput, presentTool, type ToolStatus } from './toolPresentation'
import { DurationLabel, useOperationTiming } from '../../timing/OperationTimings'

export function StreamingToolCallsView({ tools, pendingStatus = 'running' }: { tools: AgentEvent[]; pendingStatus?: ToolStatus }) {
    return <div className="agent-activity output-activity"><div className="output-tool-list">
        {groupToolEvents(tools).map((activity) => <ToolCard
            key={activity.key}
            name={activity.call?.name || activity.result?.name || ''}
            input={activity.call?.input}
            result={activity.result?.result}
            isError={activity.result?.isError}
            pendingStatus={pendingStatus}
            turnId={activity.call?.turnId || activity.result?.turnId || ''}
            callId={activity.call?.callId || activity.result?.callId}
        />)}
    </div></div>
}

export function ToolCallBlock({ block, result, turnId }: { block: AgentMessageBlock; result?: AgentMessageBlock; turnId: string }) {
    return <ToolCard name={block.name || ''} input={block.input} result={result ? result.text || '' : undefined} isError={result?.isError} pendingStatus="unknown" turnId={turnId} callId={block.callId} />
}

export function ToolResultBlock({ block, turnId }: { block: AgentMessageBlock; turnId: string }) {
    return <ToolCard name={block.name || ''} result={block.text || ''} isError={block.isError} pendingStatus="unknown" turnId={turnId} callId={block.callId} />
}

function ToolCard({ name, input, result, isError = false, pendingStatus = 'running', turnId, callId }: {
    name: string; input?: unknown; result?: string; isError?: boolean; pendingStatus?: ToolStatus; turnId: string; callId?: string
}) {
    const record = useOperationTiming(turnId, 'tool', callId)
    const duration = <DurationLabel record={record} />
    const tool = presentTool(name, input, result, isError, pendingStatus)
    if (result === undefined) {
        if (record?.status === 'failed') tool.status = 'failed'
        else if (record?.status === 'cancelled' || record?.status === 'interrupted') tool.status = 'cancelled'
        else if (record?.status === 'running') tool.status = 'running'
        else if (record?.status === 'completed') tool.status = 'unknown'
        else if (pendingStatus === 'running') tool.status = 'queued'
    }
    if (name === 'bash' || name === 'run_command') return <TerminalOutput tool={tool} duration={duration} />
    if (name === 'apply_patch' || name === 'write_file') return <FileChanges tool={tool} duration={duration} />
    return <details className="output-fold output-tool" data-status={tool.status} open={tool.status === 'running' || tool.status === 'failed' || tool.status === 'timeout' ? true : undefined}>
        <summary className="output-fold-button output-tool-header">
            <span className="output-tool-summary"><strong>{tool.name || tool.label || '工具'}</strong><code title={tool.summary}>{tool.summary}</code></span>
            <OutputStatus status={tool.status} label={tool.notice.startsWith('搜索无结果') ? '无匹配' : undefined} />
            {duration}
            <ChevronDown className="output-chevron" size={14} aria-hidden="true" />
        </summary>
        <div className="output-tool-body">
            {input !== undefined && <section className="output-section output-input">
                <div className="output-section-label">调用参数</div>
                <pre className="output-text">{formatToolInput(input)}</pre>
            </section>}
            <section className="output-section output-result">
                <div className="output-section-label">执行输出</div>
                {result === undefined ? <p className="output-muted">{tool.status === 'running' ? '正在执行，等待工具返回结果…' : '暂无输出'}</p>
                    : tool.content ? <ToolResultContent content={tool.content} /> : <p className="output-muted">暂无输出</p>}
                {typeof tool.result.size === 'number' && <div className="output-meta"><span>{tool.result.size} 字节</span></div>}
            </section>
        </div>
    </details>
}

type ToolActivity = { key: string; call?: AgentEvent; result?: AgentEvent }

function groupToolEvents(events: AgentEvent[]): ToolActivity[] {
    const activities: ToolActivity[] = []
    const byCallID = new Map<string, ToolActivity>()
    events.forEach((event, index) => {
        if (event.kind !== 'tool_call' && event.kind !== 'tool_result') return
        const key = `${event.turnId}:${event.step}:${event.callId || `tool-${index}`}`
        let activity = byCallID.get(key)
        if (!activity) {
            activity = { key }
            byCallID.set(key, activity)
            activities.push(activity)
        }
        if (event.kind === 'tool_call') activity.call = event
        else activity.result = event
    })
    return activities
}
