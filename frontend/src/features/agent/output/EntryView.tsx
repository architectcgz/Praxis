import { memo, type ReactNode } from 'react'
import { Archive, ChevronDown } from 'lucide-react'
import type { AgentEvent, HistoryMessage, MessageBlock, ModelUsage } from '../../../api'
import { DurationLabel } from '../../timing/DurationLabel'
import { AgentDirectiveCard, isHandoffTool, parseSubagentReport, SubagentReportCard, type AgentDirectory } from './HandoffViews'
import { MessageMarkdown } from './MarkdownViews'
import { CopyIconButton } from './CopyIconButton'
import { ThinkingDetails } from './ThinkingDetails'
import { ToolCallBlock, ToolResultBlock, type PairedToolResult } from './ToolCallViews'
import { MessageTiming } from './MessageTiming'

export const EntryView = memo(function EntryView({
    agentName,
    agentDirectory,
    item,
    events = [],
    pairedResults,
    usage,
    continuation = false,
    hasContinuation = false,
    copyContent,
    onOpenSubagent,
}: {
    agentName: string
    agentDirectory?: AgentDirectory
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
                    {resultBlocks.map((block, index) => (
                        <ToolResultBlock
                            block={block}
                            runId={message.runId || ''}
                            timing={block.timing || message.timing}
                            agentDirectory={agentDirectory}
                            onOpenSubagent={onOpenSubagent}
                            key={`${block.callId}-${index}`}
                        />
                    ))}
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

    // 代理间交接消息（子代理回传报告 / 子代理收到的主代理委派与指令）独立于用户气泡渲染。
    if (message.role === 'user' && message.authorKind === 'agent') {
        const formattedTime = formatMessageTime(message.createdAt)
        const parsedReport = parseSubagentReport(content)
        if (parsedReport) {
            return (
                <SubagentReportCard
                    report={parsedReport}
                    createdAt={message.createdAt}
                    formattedTime={formattedTime}
                    agentDirectory={agentDirectory}
                    onOpenSubagent={onOpenSubagent}
                />
            )
        }
        return <AgentDirectiveCard content={content} createdAt={message.createdAt} formattedTime={formattedTime} />
    }

    const messageRole = message.role
    const messageClass = messageRole === 'assistant' || messageRole === 'user' ? messageRole : 'unknown'
    let author = '未知来源'
    if (messageRole === 'assistant') author = agentName
    if (messageRole === 'user') author = '你'
    const contentToCopy = copyContent ?? content
    const segments = messageRole === 'assistant' ? groupAssistantBlocks(messageBlocks) : []
    return (
        <article className={`message message-${messageClass}${hasContinuation ? ' message-continued' : ''}`}>
            {!continuation && (
                <div className="message-meta">
                    <strong>{author}</strong>
                    <time dateTime={message.createdAt} title={message.createdAt}>{formatMessageTime(message.createdAt)}</time>
                </div>
            )}
            {messageRole === 'assistant' ? (
                <>
                    {segments.length > 0 ? segments.map((segment) => {
                        if (segment.kind === 'thinking') return <ThinkingDetails content={segment.text} key={segment.key} />
                        if (segment.kind === 'text') return <MessageMarkdown content={segment.text} key={segment.key} />
                        if (segment.kind === 'handoff') {
                            return (
                                <div className="handoff-slot" key={segment.key}>
                                    <ToolCallBlock
                                        block={segment.block}
                                        runId={message.runId || ''}
                                        events={events}
                                        pairedResult={segment.block.callId ? pairedResults?.[segment.block.callId] : undefined}
                                        agentDirectory={agentDirectory}
                                        onOpenSubagent={onOpenSubagent}
                                    />
                                </div>
                            )
                        }
                        // 连续普通工具调用合并进同一张方案 A 统一细线账本。
                        return (
                            <div className="acts" key={segment.key}>
                                {segment.blocks.map((block, index) => (
                                    <ToolCallBlock
                                        block={block}
                                        runId={message.runId || ''}
                                        events={events}
                                        pairedResult={block.callId ? pairedResults?.[block.callId] : undefined}
                                        agentDirectory={agentDirectory}
                                        onOpenSubagent={onOpenSubagent}
                                        key={`${block.callId}-${index}`}
                                    />
                                ))}
                            </div>
                        )
                    }) : content && <MessageMarkdown content={content} />}
                </>
            ) : content && <p>{content}</p>}
            <MessageActions content={hasContinuation ? '' : contentToCopy}>
                {messageRole === 'assistant' && <MessageTiming usage={usage} record={message.timing} />}
            </MessageActions>
        </article>
    )
})

type AssistantSegment =
    | { key: string; kind: 'thinking'; text: string }
    | { key: string; kind: 'text'; text: string }
    | { key: string; kind: 'handoff'; block: MessageBlock }
    | { key: string; kind: 'tools'; blocks: MessageBlock[] }

/** 相邻的普通工具调用归入同一张统一细线账本，代理交接工具独立呈现以保持清晰层级。 */
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
        if (isHandoffTool(block.name)) {
            segments.push({ key: `handoff-${block.callId || index}`, kind: 'handoff', block })
            return
        }
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
            {content && (
                <CopyIconButton
                    className="message-copy-button"
                    copiedLabel="消息已复制"
                    label="复制消息"
                    getText={() => content}
                />
            )}
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
