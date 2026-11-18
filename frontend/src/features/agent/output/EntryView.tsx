import { memo, type ReactNode } from 'react'
import { Archive, ChevronDown, CircleAlert } from 'lucide-react'
import type { AgentEvent, HistoryMessage, MessageBlock, ModelUsage } from '../../../api'
import { DurationLabel } from '../../timing/DurationLabel'
import { MessageMarkdown } from './MarkdownViews'
import { CopyIconButton } from './CopyIconButton'
import { ThinkingDetails } from './ThinkingDetails'
import { ToolCallBlock, ToolResultBlock, type PairedToolResult } from './ToolCallViews'
import { MessageTiming } from './MessageTiming'

export const EntryView = memo(function EntryView({ agentName, item, events = [], pairedResults, usage, continuation = false, hasContinuation = false, copyContent, onOpenSubagent }: {
    agentName: string
    item: HistoryMessage
    events?: AgentEvent[]
    pairedResults?: Record<string, PairedToolResult>
    usage?: ModelUsage
    continuation?: boolean
    hasContinuation?: boolean
    copyContent?: string
    onOpenSubagent: (id: string) => void
}) {
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
                    {resultBlocks.map((block, index) => <ToolResultBlock block={block} runId={message.runId || ''} timing={block.timing || message.timing} onOpenSubagent={onOpenSubagent} key={`${block.callId}-${index}`} />)}
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

    const messageRole = message.role
    const messageClass = messageRole === 'assistant' || messageRole === 'user' ? messageRole : 'unknown'
    let author = '未知来源'
    if (messageRole === 'assistant') author = agentName
    if (messageRole === 'user') author = message.authorKind === 'agent' ? '代理' : '你'
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
                                onOpenSubagent={onOpenSubagent}
                                key={`${block.callId}-${index}`}
                            />)}
                        </div>
                    }) : content && <MessageMarkdown content={content} />}
                </>
            ) : content && <p>{content}</p>}
            {message.error && <div className="message-error-box" role="alert"><CircleAlert size={14} strokeWidth={1.8} aria-hidden="true" /><span>{message.error}</span></div>}
            <MessageActions content={hasContinuation ? '' : contentToCopy}>
                {messageRole === 'assistant' && <MessageTiming usage={usage} record={message.timing} />}
            </MessageActions>
        </article>
    )
})

type AssistantSegment =
    | { key: string; kind: 'thinking'; text: string }
    | { key: string; kind: 'text'; text: string }
    | { key: string; kind: 'tools'; blocks: MessageBlock[] }

/** 相邻的工具调用归入同一张活动账本，跳过纯空白块以免打断连续工具并留下空节点。 */
function groupAssistantBlocks(blocks: MessageBlock[]): AssistantSegment[] {
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
