import { Children, isValidElement, memo, useEffect, useRef, useState, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { Check, ChevronRight, Copy, LoaderCircle } from 'lucide-react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { AgentEvent, AgentHistoryItem, AgentMessageBlock } from '../../api'

const markdownPlugins = [remarkGfm]
const markdownComponents: Components = {
    table: MarkdownTable,
}

const codeLanguageLabels: Record<string, string> = {
    bash: 'Shell',
    css: 'CSS',
    go: 'Go',
    html: 'HTML',
    javascript: 'JavaScript',
    js: 'JavaScript',
    json: 'JSON',
    markdown: 'Markdown',
    md: 'Markdown',
    py: 'Python',
    python: 'Python',
    sh: 'Shell',
    shell: 'Shell',
    sql: 'SQL',
    ts: 'TypeScript',
    tsx: 'TSX',
    yaml: 'YAML',
    yml: 'YAML',
}

let dateFormatter: Intl.DateTimeFormat | null = null

const failureMessages: Record<string, string> = {
    provider_unavailable: '代理无法响应，因为没有配置模型提供者。',
    execution_provider_error: '模型提供者无法完成请求。',
    execution_tool_error: '代理无法响应，因为必需的工具失败了。',
    execution_policy_blocked: '请求被配置的执行策略阻止。',
    execution_approval_required: '代理正在等待批准才能响应。',
    execution_storage_error: '代理无法响应，因为无法保存其持久状态。',
    runtime_cancelled: '执行在代理响应之前被取消。',
    runtime_invalid_outcome: '代理返回了无效的执行结果。',
    runtime_failed: '代理运行时在响应之前失败。',
}

export const StreamingOutputView = memo(function StreamingOutputView({ agentProfile, content }: { agentProfile: string; content: string }) {
    return (
        <article className="message message-assistant" aria-live="polite">
            <div className="message-meta">
                <strong>{agentProfile}</strong>
            </div>
            <MessageMarkdown content={content} streaming />
        </article>
    )
})

const MessageMarkdown = memo(function MessageMarkdown({ content, streaming = false }: { content: string; streaming?: boolean }) {
    return (
        <div className="message-markdown">
            <ReactMarkdown components={{ ...markdownComponents, pre: (props) => <MarkdownCodeBlock {...props} streaming={streaming} /> }} remarkPlugins={markdownPlugins} skipHtml>
                {content}
            </ReactMarkdown>
        </div>
    )
})

function MarkdownCodeBlock({ children, streaming }: ComponentPropsWithoutRef<'pre'> & { streaming: boolean }) {
    const code = Children.toArray(children)[0]
    if (!isValidElement<ComponentPropsWithoutRef<'code'>>(code)) {
        return <pre>{children}</pre>
    }
    const language = codeLanguageLabel(code.props.className)
    const content = extractTextContent(code.props.children).replace(/\n$/, '')
    const { added, removed } = codeLineChanges(content, code.props.className)

    return (
        <div className="markdown-code-block">
            <details open={streaming || undefined}>
                <summary className="markdown-code-toolbar">
                    <ChevronRight size={14} strokeWidth={1.8} aria-hidden="true" />
                    <span className="markdown-code-language">{language}</span>
                    <span className="markdown-code-changes"><span>+{added}</span><span>-{removed}</span></span>
                </summary>
                <pre>{code}</pre>
            </details>
            <CopyIconButton
                className="markdown-code-copy-button"
                copiedLabel={`${language} 代码已复制`}
                label={`复制 ${language} 代码`}
                onCopy={() => copyText(content)}
            />
        </div>
    )
}

function codeLineChanges(content: string, className?: string) {
    const lines = content ? content.split('\n') : []
    const isDiff = /language-(diff|patch)\b/i.test(className || '') || lines.some((line) => line.startsWith('diff --git ') || line.startsWith('@@ ') || line.startsWith('*** Begin Patch'))
    if (!isDiff) return { added: lines.length, removed: 0 }
    return {
        added: lines.filter((line) => line.startsWith('+') && !line.startsWith('+++')).length,
        removed: lines.filter((line) => line.startsWith('-') && !line.startsWith('---')).length,
    }
}

function MarkdownTable({ children }: ComponentPropsWithoutRef<'table'>) {
    const tableRef = useRef<HTMLTableElement>(null)

    return (
        <div className="markdown-table">
            <div className="markdown-table-toolbar">
                <span className="markdown-table-label">表格</span>
                <CopyIconButton
                    className="markdown-table-copy-button"
                    copiedLabel="表格已复制"
                    label="复制表格为制表符分隔文本"
                    onCopy={() => copyText(tableText(tableRef.current))}
                />
            </div>
            <div className="markdown-table-scroll">
                <table ref={tableRef}>{children}</table>
            </div>
        </div>
    )
}

function codeLanguageLabel(className: string | undefined) {
    const language = className?.match(/language-([a-z0-9+-]+)/i)?.[1]?.toLowerCase()
    if (!language) {
        return '纯文本'
    }
    return codeLanguageLabels[language] || language.toUpperCase()
}

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

export function PendingOutputView({ agentProfile }: { agentProfile: string }) {
    return (
        <article className="message message-assistant message-pending" role="status">
            <div className="message-meta">
                <strong>{agentProfile}</strong>
            </div>
            <div className="message-pending-status">
                <LoaderCircle size={16} strokeWidth={1.8} aria-hidden="true" />
                <span>思考中</span>
            </div>
        </article>
    )
}

export const HistoryItemView = memo(function HistoryItemView({ agentProfile, item, toolResults }: { agentProfile: string; item: AgentHistoryItem; toolResults?: AgentMessageBlock[] }) {
    if (item.kind === 'execution' && item.execution) {
        const content = failureMessage(item.execution.failureCode)
        return (
            <article className="message message-error">
                <div className="message-meta">
                    <strong>Praxis</strong>
                    <time dateTime={item.at} title={item.at}>{formatMessageTime(item.at)}</time>
                </div>
                <p>{content}</p>
                <MessageCopyButton content={content} />
            </article>
        )
    }
    if (!item.message) {
        return null
    }
    const messageBlocks = item.message.blocks || []
    const resultBlocks = messageBlocks.filter((block) => block.kind === 'tool_result')
    if (item.message.role === 'tool') {
        return (
            <article className="message message-tool">
                <div className="message-meta">
                    <strong>工具输出</strong>
                    <time dateTime={item.message.at} title={item.message.at}>{formatMessageTime(item.message.at)}</time>
                </div>
                {resultBlocks.map((block, index) => <ToolResultBlock block={block} key={`${block.callId}-${index}`} />)}
            </article>
        )
    }
    return (
        <article className={`message message-${item.message.role}`}>
            <div className="message-meta">
                <strong>{item.message.role === 'assistant' ? agentProfile : '你'}</strong>
                <time dateTime={item.message.at} title={item.message.at}>{formatMessageTime(item.message.at)}</time>
            </div>
            {item.message.role === 'assistant' ? (
                <>
                    {item.message.thinking && <ThinkingDetails content={item.message.thinking} />}
                    {messageBlocks.length > 0 ? messageBlocks.map((block, index) => {
                        if (block.kind === 'thinking') return <ThinkingDetails content={block.text || ''} key={index} />
                        if (block.kind === 'text') return <MessageMarkdown content={block.text || ''} key={index} />
                        if (block.kind === 'tool_call') return (
                            <div className="message-tool-calls" key={index}>
                                <ToolCallBlock block={block} result={toolResults?.find((result) => result.callId === block.callId)} />
                            </div>
                        )
                        return null
                    }) : item.message.content && <MessageMarkdown content={item.message.content} />}
                </>
            ) : item.message.content && <p>{item.message.content}</p>}
            {item.message.content && <MessageCopyButton content={item.message.content} />}
        </article>
    )
}, (prev, next) => {
    if (prev.agentProfile !== next.agentProfile) return false
    if (prev.item.kind !== next.item.kind) return false
    if (prev.item.sequence !== next.item.sequence) return false
    if (prev.item.at !== next.item.at) return false
    if (prev.item.message?.content !== next.item.message?.content) return false
    if (prev.item.message?.thinking !== next.item.message?.thinking) return false
    if (messageBlocksKey(prev.item.message?.blocks) !== messageBlocksKey(next.item.message?.blocks)) return false
    if (messageBlocksKey(prev.toolResults) !== messageBlocksKey(next.toolResults)) return false
    if (prev.item.execution?.id !== next.item.execution?.id) return false
    return true
})

export const AgentActivityView = memo(function AgentActivityView({ thinking, error }: { thinking?: string; error?: string }) {
    return (
        <div className="agent-activity" aria-live="polite">
            {thinking && <ThinkingDetails content={thinking} streaming />}
            {error && <p role="alert">{error}</p>}
        </div>
    )
})

export function StreamingTurnView({ agentProfile, events }: { agentProfile: string; events: AgentEvent[] }) {
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
        if (event.kind === 'thinking_delta' && event.text) {
            segments.push(<AgentActivityView thinking={event.text} key={`thinking-${index}`} />)
        } else if (event.kind === 'text_delta' && event.text) {
            segments.push(<StreamingOutputView agentProfile={agentProfile} content={event.text} key={`text-${index}`} />)
        }
    })
    if (tools.length > 0) {
        segments.push(<StreamingToolCallsView tools={tools} key={`tools-${events.length}`} />)
    }
    return <div className="streaming-turn">{segments}</div>
}

function ThinkingDetails({ content, streaming = false }: { content: string; streaming?: boolean }) {
    // 相邻的加粗片段缺少分隔时，Markdown 会把四个星号显示为正文。
    const formattedContent = content.replace(/(\S)\*{4}(?=\S)/g, '$1**\n\n**')
    return (
        <details className="thinking-details" open={streaming || undefined}>
            <summary><ChevronRight size={14} strokeWidth={1.8} aria-hidden="true" /><span>思考过程</span></summary>
            <div className="thinking-content"><MessageMarkdown content={formattedContent} streaming={streaming} /></div>
        </details>
    )
}

export function StreamingToolCallsView({ tools }: { tools: AgentEvent[] }) {
    return <div className="agent-activity" aria-live="polite"><ToolActivityList events={tools} /></div>
}

function ToolCallBlock({ block, result }: { block: AgentMessageBlock; result?: AgentMessageBlock }) {
    return (
        <details className="tool-call">
            <summary>调用 {block.name || '工具'}</summary>
            <pre>{formatToolInput(block.input)}</pre>
            {result && <ToolResultBlock block={result} />}
        </details>
    )
}

function ToolResultBlock({ block }: { block: AgentMessageBlock }) {
    return (
        <div className={`tool-result ${block.isError ? 'tool-result-error' : ''}`}>
            <div className="tool-result-label">{block.isError ? '工具失败' : '工具输出'} {block.name || ''}</div>
            <pre>{block.text || '（无输出）'}</pre>
        </div>
    )
}

type ToolActivity = {
    key: string
    call?: AgentEvent
    result?: AgentEvent
}

function ToolActivityList({ events }: { events: AgentEvent[] }) {
    const activities = groupToolEvents(events)
    return (
        <div className="agent-tool-list">
            {activities.map((activity) => {
                const name = activity.call?.name || activity.result?.name || '工具'
                return (
                    <details className="agent-tool" key={activity.key} open>
                        <summary>调用 {name}</summary>
                        {activity.call && <pre>{formatToolInput(activity.call.input)}</pre>}
                        {activity.result ? (
                            <div className={`tool-result ${activity.result.isError ? 'tool-result-error' : ''}`}>
                                <div className="tool-result-label">{activity.result.isError ? '工具失败' : '工具输出'} {name}</div>
                                <pre>{activity.result.result || '（无输出）'}</pre>
                            </div>
                        ) : (
                            <div className="tool-result-label">执行中</div>
                        )}
                    </details>
                )
            })}
        </div>
    )
}

function groupToolEvents(events: AgentEvent[]): ToolActivity[] {
    const activities: ToolActivity[] = []
    const byCallID = new Map<string, ToolActivity>()
    events.forEach((event, index) => {
        const callID = event.callId || `tool-${index}`
        let activity = byCallID.get(callID)
        if (!activity) {
            activity = { key: `${event.turn}-${callID}`, call: event.kind === 'tool_call' ? event : undefined, result: event.kind === 'tool_result' ? event : undefined }
            byCallID.set(callID, activity)
            activities.push(activity)
            return
        }
        if (event.kind === 'tool_call') {
            activity.call = event
        } else {
            activity.result = event
        }
    })
    return activities
}

function formatToolInput(input: unknown) {
    if (typeof input === 'string') {
        return input
    }
    try {
        return JSON.stringify(input ?? {}, null, 2) || '{}'
    } catch {
        return String(input)
    }
}

function messageBlocksKey(blocks: AgentMessageBlock[] | undefined) {
    return blocks?.map((block) => `${block.kind}:${block.callId || ''}:${block.name || ''}:${block.text || ''}:${JSON.stringify(block.input)}`).join('|') || ''
}

function MessageCopyButton({ content }: { content: string }) {
    return (
        <div className="message-copy-actions">
            <CopyIconButton
                className="message-copy-button"
                copiedLabel="消息已复制"
                label="复制消息"
                onCopy={() => copyText(content)}
            />
        </div>
    )
}

type CopyIconButtonProps = {
    className: string
    copiedLabel: string
    label: string
    onCopy: () => Promise<void>
}

function CopyIconButton({ className, copiedLabel, label, onCopy }: CopyIconButtonProps) {
    const [copied, setCopied] = useState(false)

    useEffect(() => {
        if (!copied) {
            return
        }
        const timer = window.setTimeout(() => setCopied(false), 1600)
        return () => window.clearTimeout(timer)
    }, [copied])

    const copy = async () => {
        try {
            await onCopy()
            setCopied(true)
        } catch {
            setCopied(false)
        }
    }

    return (
        <button
            className={`${className} ${copied ? 'copied' : ''}`}
            type="button"
            title={copied ? copiedLabel : label}
            aria-label={copied ? copiedLabel : label}
            onClick={() => void copy()}
        >
            {copied ? <Check size={15} strokeWidth={2} aria-hidden="true" /> : <Copy size={15} strokeWidth={1.8} aria-hidden="true" />}
        </button>
    )
}

async function copyText(content: string) {
    if (!navigator.clipboard?.writeText) {
        throw new Error('Clipboard access is unavailable')
    }
    await navigator.clipboard.writeText(content)
}

function tableText(table: HTMLTableElement | null) {
    if (!table) {
        return ''
    }
    return Array.from(table.rows, (row) => (
        Array.from(row.cells, (cell) => cell.textContent?.replace(/\s+/g, ' ').trim() || '').join('\t')
    )).join('\n')
}

function failureMessage(code: string) {
    return failureMessages[code] || `代理无法响应。失败代码: ${code}`
}

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

function extractTextContent(children: ReactNode): string {
    if (typeof children === 'string') {
        return children
    }
    if (Array.isArray(children)) {
        return children.map(extractTextContent).join('')
    }
    if (isValidElement(children)) {
        const props = children.props as { children?: ReactNode }
        if (props.children) {
            return extractTextContent(props.children)
        }
    }
    return ''
}
