import { memo, type ReactNode } from 'react'
import type { AgentEvent, Entry, EntryBlock } from '../../../api'
import { DurationLabel } from '../../timing/DurationLabel'
import { MessageMarkdown } from './MarkdownViews'
import { CopyIconButton } from './CopyIconButton'
import { ThinkingDetails } from './ThinkingDetails'
import { ToolCallBlock, ToolResultBlock } from './ToolCallViews'
import { MessageTiming } from './MessageTiming'

export const EntryView = memo(function EntryView({ agentName, item, events = [], continuation = false, hasContinuation = false, copyContent }: {
    agentName: string
    item: Entry
    events?: AgentEvent[]
    continuation?: boolean
    hasContinuation?: boolean
    copyContent?: string
}) {
    if (item.kind === 'run') return null
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
                    {resultBlocks.map((block, index) => <ToolResultBlock block={block} runId={message.runId || ''} timing={message.timing} key={`${block.callId}-${index}`} />)}
                </div>
            </article>
        )
    }
    const messageRole = message.role
    const messageClass = messageRole === 'assistant' || messageRole === 'user' ? messageRole : 'unknown'
    const author = message.kind === 'summary' ? '上下文摘要' : messageRole === 'assistant' ? agentName : messageRole === 'user' ? '你' : '未知来源'
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
                                key={`${block.callId}-${index}`}
                            />)}
                        </div>
                    }) : content && <MessageMarkdown content={content} />}
                </>
            ) : content && <p>{content}</p>}
            {message.error && <p role="alert">{message.error}</p>}
            <MessageActions content={hasContinuation ? '' : contentToCopy}>
                {message.kind === 'summary'
                    ? <DurationLabel record={message.timing} label="压缩耗时" />
                    : messageRole === 'assistant' && <MessageTiming runId={message.runId || ''} turnId={message.turnId} record={message.timing} />}
            </MessageActions>
        </article>
    )
})

type AssistantSegment =
    | { key: string; kind: 'thinking'; text: string }
    | { key: string; kind: 'text'; text: string }
    | { key: string; kind: 'tools'; blocks: EntryBlock[] }

/** 相邻的工具调用归入同一张活动账本，正文与思考保持原有先后。 */
function groupAssistantBlocks(blocks: EntryBlock[]): AssistantSegment[] {
    const segments: AssistantSegment[] = []
    blocks.forEach((block, index) => {
        if (block.kind === 'thinking') {
            segments.push({ key: `thinking-${index}`, kind: 'thinking', text: block.text || '' })
            return
        }
        if (block.kind === 'text') {
            segments.push({ key: `text-${index}`, kind: 'text', text: block.text || '' })
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
