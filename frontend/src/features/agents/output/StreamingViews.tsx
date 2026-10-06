import { memo, type ReactNode } from 'react'
import { LoaderCircle } from 'lucide-react'
import type { AgentEvent } from '../../../api'
import { MessageMarkdown } from './MarkdownViews'
import { StreamingToolCallsView } from './ToolCallViews'
import { ThinkingDetails } from './ThinkingDetails'
import { AgentRuntime, MessageTiming } from '../../timing/OperationTimings'

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

export function PendingOutputView({ agentProfile, turnId }: { agentProfile: string; turnId: string }) {
    return (
        <article className="message message-assistant message-pending" role="status">
            <div className="message-meta">
                <strong>{agentProfile}</strong>
                <AgentRuntime turnId={turnId} />
            </div>
            <div className="message-pending-status">
                <LoaderCircle size={16} strokeWidth={1.8} aria-hidden="true" />
                <span>思考中</span>
            </div>
        </article>
    )
}

export const AgentActivityView = memo(function AgentActivityView({ thinking, providerWaiting, error }: { thinking?: string; providerWaiting?: boolean; error?: string }) {
    return (
        <div className="agent-activity" aria-live="polite">
            {providerWaiting && <div className="message-pending-status"><LoaderCircle size={16} strokeWidth={1.8} aria-hidden="true" /><span>等待模型响应</span></div>}
            {thinking && <ThinkingDetails content={thinking} streaming />}
            {error && <p role="alert">{error}</p>}
        </div>
    )
})

export function StreamingStepView({ agentProfile, events, turnId, step, active, continuation = false, hasContinuation = false }: {
    agentProfile: string
    events: AgentEvent[]
    turnId: string
    step: number
    active: boolean
    continuation?: boolean
    hasContinuation?: boolean
}) {
    const segments: ReactNode[] = []
    let tools: AgentEvent[] = []
    events.forEach((event, index) => {
        if (event.kind === 'tool_call' || event.kind === 'tool_result') {
            tools.push(event)
            return
        }
        if (tools.length > 0) {
            segments.push(<StreamingToolCallsView tools={tools} key={`tools-${index}`} />)
            tools = []
        }
        if (event.kind === 'provider_waiting' && active && index === events.length - 1) {
            segments.push(<AgentActivityView providerWaiting key={`provider-waiting-${index}`} />)
        } else if (event.kind === 'thinking_delta' && event.text) {
            segments.push(<AgentActivityView thinking={event.text} key={`thinking-${index}`} />)
        } else if (event.kind === 'text_delta' && event.text) {
            segments.push(<StreamingOutputView content={event.text} key={`text-${index}`} />)
        }
    })
    if (tools.length > 0) segments.push(<StreamingToolCallsView tools={tools} key={`tools-${events.length}`} />)
    return <div className={`streaming-step${hasContinuation ? ' streaming-step-continued' : ''}`}>
        {!continuation && <div className="message message-assistant streaming-step-header">
            <div className="message-meta">
                <strong>{agentProfile}</strong>
                {active && <AgentRuntime turnId={turnId} />}
            </div>
        </div>}
        {segments}
        <div className="agent-activity">
            <MessageTiming turnId={turnId} referenceId={`assistant:${turnId}:${step}`} />
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
