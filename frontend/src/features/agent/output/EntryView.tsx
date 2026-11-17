import { memo, type ReactNode } from 'react'
import { Archive, Bot, ChevronDown, CircleAlert, Send } from 'lucide-react'
import type { AgentEvent, Entry, EntryBlock } from '../../../api'
import { DurationLabel } from '../../timing/DurationLabel'
import { MessageMarkdown } from './MarkdownViews'
import { CopyIconButton } from './CopyIconButton'
import { ThinkingDetails } from './ThinkingDetails'
import { ToolCallBlock, ToolResultBlock, type PairedToolResult } from './ToolCallViews'
import { MessageTiming } from './MessageTiming'

const requestKindLabels: Record<string, string> = {
    iteration: '模型迭代',
    risk_review: '风险审核',
    summary: '上下文摘要',
}

const reportOutcomeLabels: Record<string, { label: string; tone: string }> = {
    yielded: { label: '已交付', tone: 'ok' },
    completed: { label: '已完成', tone: 'ok' },
    progress: { label: '进展汇报', tone: 'info' },
    failed: { label: '失败', tone: 'bad' },
    interrupted: { label: '已中断', tone: 'warn' },
}

export const EntryView = memo(function EntryView({ agentName, item, events = [], pairedResults, continuation = false, hasContinuation = false, copyContent }: {
    agentName: string
    item: Entry
    events?: AgentEvent[]
    pairedResults?: Record<string, PairedToolResult>
    continuation?: boolean
    hasContinuation?: boolean
    copyContent?: string
}) {
    if (item.kind === 'run') return null
    if (item.kind === 'model_request') {
        const statusText = item.status === 'interrupted' ? '已中断' : '失败'
        const requestLabel = requestKindLabels[item.requestKind || ''] || item.requestKind || '模型请求'
        return <details className="message entry-model-request">
            <summary className="entry-model-request-summary">
                <CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" />
                <span className="entry-model-request-title">{requestLabel}{statusText} · 第 {(item.retryCount || 0) + 1} 次尝试</span>
                <DurationLabel record={item.timing} label="" compact />
                <ChevronDown className="entry-model-request-chevron" size={14} aria-hidden="true" />
            </summary>
            <div className="entry-model-request-body">
                {item.error && <div className="message-error-box" role="alert"><CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" /><span>{item.error}</span></div>}
                {item.content.length > 0 && <div className="entry-model-request-partial">
                    <span className="entry-section-kicker">本次请求的部分输出</span>
                    <MessageMarkdown content={item.content.map((block) => block.text || '').join('')} />
                </div>}
            </div>
        </details>
    }
    const message = item
    const content = message.content.filter((block) => block.kind === 'text').map((block) => block.text || '').join('')
    const messageBlocks = message.content
    const resultBlocks = messageBlocks.filter((block) => block.kind === 'tool_result')
    if (message.role === 'tool') {
        return (
            <article className="message message-tool">
                <div className="message-meta">
                    <strong>工具输出</strong>
                    <time dateTime={message.createdAt} title={message.createdAt}>{formatMessageTime(message.createdAt)}</time>
                </div>
                <div className="acts">
                    {resultBlocks.map((block, index) => <ToolResultBlock block={block} runId={message.runId || ''} timing={block.timing || message.timing} key={`${block.callId}-${index}`} />)}
                </div>
            </article>
        )
    }

    if (message.kind === 'summary') {
        return (
            <article className="message entry-system-card entry-summary-card">
                <details className="entry-summary-details">
                    <summary className="entry-system-head">
                        <Archive size={14} strokeWidth={1.8} aria-hidden="true" />
                        <strong>上下文摘要</strong>
                        <span className="entry-system-badge">已压缩早期历史</span>
                        <time dateTime={message.createdAt} title={message.createdAt}>{formatMessageTime(message.createdAt)}</time>
                        <DurationLabel record={message.timing} label="" compact />
                        <ChevronDown className="entry-summary-chevron" size={14} aria-hidden="true" />
                    </summary>
                    <div className="entry-summary-body">
                        {content ? <MessageMarkdown content={content} /> : <p className="output-muted">摘要内容为空</p>}
                    </div>
                </details>
            </article>
        )
    }

    if (message.kind === 'subagent_report') {
        const parsed = parseSubagentReport(content)
        const outcomeInfo = parsed.outcome ? reportOutcomeLabels[parsed.outcome] || { label: parsed.outcome, tone: '' } : null
        return (
            <article className="message entry-system-card entry-subagent-report">
                <div className="entry-system-head">
                    <Bot size={14} strokeWidth={1.8} aria-hidden="true" />
                    <strong>子代理汇报</strong>
                    {outcomeInfo && <span className="output-chip" data-tone={outcomeInfo.tone}><span className="output-chip-dot" aria-hidden="true" />{outcomeInfo.label}</span>}
                    {parsed.partial && <span className="output-chip">阶段汇报</span>}
                    <time dateTime={message.createdAt} title={message.createdAt}>{formatMessageTime(message.createdAt)}</time>
                </div>
                {(parsed.agentId || parsed.failureCode) && (
                    <div className="entry-system-meta">
                        {parsed.agentId && <span>代理 <code>{parsed.agentId}</code></span>}
                        {parsed.runId && <span>执行 <code>{parsed.runId}</code></span>}
                        {parsed.failureCode && <span>原因 <code>{parsed.failureCode}</code></span>}
                    </div>
                )}
                {parsed.body && <MessageMarkdown content={parsed.body} />}
                {message.error && <div className="message-error-box" role="alert"><CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" /><span>{message.error}</span></div>}
                <MessageActions content={parsed.body || content} />
            </article>
        )
    }

    if (message.kind === 'instruction') {
        return (
            <article className="message entry-system-card entry-instruction-card">
                <div className="entry-system-head">
                    <Send size={13} strokeWidth={1.8} aria-hidden="true" />
                    <strong>主代理补充指令</strong>
                    <time dateTime={message.createdAt} title={message.createdAt}>{formatMessageTime(message.createdAt)}</time>
                </div>
                {content && <MessageMarkdown content={content} />}
                {message.error && <div className="message-error-box" role="alert"><CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" /><span>{message.error}</span></div>}
                <MessageActions content={content} />
            </article>
        )
    }

    const messageRole = message.role
    const messageClass = messageRole === 'assistant' || messageRole === 'user' ? messageRole : 'unknown'
    const author = messageRole === 'assistant' ? agentName : messageRole === 'user' ? '你' : '未知来源'
    const contentToCopy = copyContent ?? content
    const segments = messageRole === 'assistant' ? groupAssistantBlocks(messageBlocks) : []
    return (
        <article className={`message message-${messageClass}${hasContinuation ? ' message-continued' : ''}`}>
            {!continuation && <div className="message-meta">
                <strong>{author}</strong>
                <time dateTime={message.createdAt} title={message.createdAt}>{formatMessageTime(message.createdAt)}</time>
            </div>}
            {messageRole === 'assistant' ? (
                <>
                    {segments.length > 0 ? segments.map((segment) => {
                        if (segment.kind === 'thinking') return <ThinkingDetails content={segment.text} key={segment.key} />
                        if (segment.kind === 'text') return <MessageMarkdown content={segment.text} key={segment.key} />
                        // 一次运行里的连续工具调用合并成同一张活动账本。
                        return <div className="acts" key={segment.key}>
                            {segment.blocks.map((block, index) => <ToolCallBlock
                                block={block}
                                runId={message.runId || ''}
                                events={events}
                                pairedResult={block.callId ? pairedResults?.[block.callId] : undefined}
                                key={`${block.callId}-${index}`}
                            />)}
                        </div>
                    }) : content && <MessageMarkdown content={content} />}
                </>
            ) : content && <p>{content}</p>}
            {message.error && <div className="message-error-box" role="alert"><CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" /><span>{message.error}</span></div>}
            <MessageActions content={hasContinuation ? '' : contentToCopy}>
                {messageRole === 'assistant' && <MessageTiming runId={message.runId || ''} turnId={message.turnId} requestId={message.requestId} record={message.timing} />}
            </MessageActions>
        </article>
    )
})

function parseSubagentReport(raw: string) {
    const match = raw.match(/^\[Subagent report ([^\]]+)\]\s+agent=([^\s]+)\s+run=([^\s]+)\s+outcome=([^\s]+)\s+final=(true|false)\s+partial=(true|false)\s+failure=([^\n]*)\n?([\s\S]*)$/)
    if (!match) {
        return { reportId: '', agentId: '', runId: '', outcome: '', partial: false, failureCode: '', body: raw }
    }
    return {
        reportId: match[1],
        agentId: match[2],
        runId: match[3],
        outcome: match[4],
        partial: match[6] === 'true',
        failureCode: match[7].trim(),
        body: match[8].trim(),
    }
}

type AssistantSegment =
    | { key: string; kind: 'thinking'; text: string }
    | { key: string; kind: 'text'; text: string }
    | { key: string; kind: 'tools'; blocks: EntryBlock[] }

/** 相邻的工具调用归入同一张活动账本，跳过纯空白块以免打断连续工具并留下空节点。 */
function groupAssistantBlocks(blocks: EntryBlock[]): AssistantSegment[] {
    const segments: AssistantSegment[] = []
    blocks.forEach((block, index) => {
        if (block.kind === 'thinking') {
            if (block.text?.trim()) segments.push({ key: `thinking-${index}`, kind: 'thinking', text: block.text })
            return
        }
        if (block.kind === 'text') {
            if (block.text?.trim()) segments.push({ key: `text-${index}`, kind: 'text', text: block.text })
            return
        }
        if (block.kind !== 'tool_call') return
        const last = segments[segments.length - 1]
        if (last?.kind === 'tools') {
            segments[segments.length - 1] = { ...last, blocks: [...last.blocks, block] }
            return
        }
        segments.push({ key: `tools-${index}`, kind: 'tools', blocks: [block] })
    })
    return segments
}

function MessageActions({ content, children }: { content: string; children?: ReactNode }) {
    return (
        <div className="message-copy-actions">
            {children}
            {content && <CopyIconButton
                className="message-copy-button"
                copiedLabel="消息已复制"
                label="复制消息"
                getText={() => content}
            />}
        </div>
    )
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
