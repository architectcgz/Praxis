import { memo, type ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import type { AgentEvent, RunSnapshot } from '../../../api'
import { MessageMarkdown } from './MarkdownViews'
import { StreamingToolCallsView } from './ToolCallViews'
import { ThinkingDetails } from './ThinkingDetails'
import { RunDuration } from '../../timing/DurationLabel'
import { MessageTiming } from './MessageTiming'

export const StreamingOutputView = memo(function StreamingOutputView({ content }: { content: string }) {
    return (
        <article className="message message-assistant" aria-live="polite">
            <MessageMarkdown content={content} streaming />
        </article>
    )
})

export const PendingUserMessageView = memo(function PendingUserMessageView({ content, at }: { content: string; at: string }) {
    return (
        <article className="message message-user">
            <div className="message-meta">
                <strong>你</strong>
                <time dateTime={at} title={at}>{formatMessageTime(at)}</time>
            </div>
            <p>{content}</p>
        </article>
    )
})

export function PendingOutputView({ agentName, run }: { agentName: string; run?: RunSnapshot | null }) {
    return (
        <article className="message message-assistant message-pending" role="status">
            <div className="message-meta">
                <strong>{agentName}</strong>
                <RunDuration run={run} label="运行中" activeOnly />
            </div>
            <div className="message-pending-status">
                <LoaderCircle size={16} strokeWidth={1.8} aria-hidden="true" />
                <span>思考中</span>
            </div>
        </article>
    )
}

export const AgentActivityView = memo(function AgentActivityView({ thinking, streaming = false, providerWaiting, error }: { thinking?: string; streaming?: boolean; providerWaiting?: boolean; error?: string }) {
    return (
        <div className="agent-activity" aria-live="polite">
            {providerWaiting && <div className="message-pending-status"><LoaderCircle size={16} strokeWidth={1.8} aria-hidden="true" /><span>等待模型响应</span></div>}
            {thinking && <ThinkingDetails content={thinking} streaming={streaming} />}
            {error && <p role="alert">{error}</p>}
        </div>
    )
})

export function StreamingTurnView({ agentName, events, runId, run, turnId, active, continuation = false, hasContinuation = false }: {
    agentName: string
    events: AgentEvent[]
    runId: string
    run?: RunSnapshot | null
    turnId: string
    active: boolean
    continuation?: boolean
    hasContinuation?: boolean
}) {
    const started = events.findLast((event): event is Extract<AgentEvent, { kind: 'model_request_started' }> => event.kind === 'model_request_started')
    const finished = events.findLast((event): event is Extract<AgentEvent, { kind: 'model_request_finished' }> => event.kind === 'model_request_finished')
    const segments: ReactNode[] = []
    const pendingStatus = active ? 'running' : 'unknown'
    let tools: AgentEvent[] = []
    events.forEach((event, index) => {
        if (event.kind === 'tool_call' || event.kind === 'tool_started' || event.kind === 'tool_approval_required' || event.kind === 'tool_result') {
            tools.push(event)
            return
        }
        if (tools.length > 0) {
            segments.push(<StreamingToolCallsView tools={tools} pendingStatus={pendingStatus} key={`tools-${index}`} />)
            tools = []
        }
        if (event.kind === 'model_request_started' && active && index === events.length - 1) {
            segments.push(<AgentActivityView providerWaiting key={`provider-waiting-${index}`} />)
        } else if (event.kind === 'thinking_delta' && event.data.text) {
            // 只有当前末尾的思考仍在流式输出；工具或正文出现后，前一段思考结束并收起。
            segments.push(<AgentActivityView thinking={event.data.text} streaming={active && index === events.length - 1} key={`thinking-${index}`} />)
        } else if (event.kind === 'text_delta' && event.data.text) {
            segments.push(<StreamingOutputView content={event.data.text} key={`text-${index}`} />)
        }
    })
    if (tools.length > 0) segments.push(<StreamingToolCallsView tools={tools} pendingStatus={pendingStatus} key={`tools-${events.length}`} />)
    return <div className={`streaming-turn${hasContinuation ? ' streaming-turn-continued' : ''}`}>
        {!continuation && <div className="message message-assistant streaming-turn-header">
            <div className="message-meta">
                <strong>{agentName}</strong>
                <RunDuration run={run} label="运行中" activeOnly />
            </div>
        </div>}
        {segments}
        <div className="agent-activity turn-foot">
            <MessageTiming runId={runId} turnId={turnId} record={finished?.data.timing || started?.data.timing} running={active && !finished} />
        </div>
    </div>
}

let dateFormatter: Intl.DateTimeFormat | null = null

function formatMessageTime(value: string) {
    const date = new Date(value)
    if (Number.isNaN(date.getTime())) {
        return '未知时间'
    }
    if (!dateFormatter) {
        dateFormatter = new Intl.DateTimeFormat(undefined, {
            year: 'numeric',
            month: '2-digit',
            day: '2-digit',
            hour: '2-digit',
            minute: '2-digit',
            second: '2-digit',
        })
    }
    return dateFormatter.format(date)
}
