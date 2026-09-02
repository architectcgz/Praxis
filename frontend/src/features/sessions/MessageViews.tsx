import { Children, isValidElement, memo, useEffect, useRef, useState, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { Check, Copy, LoaderCircle } from 'lucide-react'
import ReactMarkdown, { type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import type { AgentHistoryItem } from '../../api'

const markdownPlugins = [remarkGfm]
const markdownComponents: Components = {
    pre: MarkdownCodeBlock,
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
            <MessageMarkdown content={content} />
        </article>
    )
})

const MessageMarkdown = memo(function MessageMarkdown({ content }: { content: string }) {
    return (
        <div className="message-markdown">
            <ReactMarkdown components={markdownComponents} remarkPlugins={markdownPlugins} skipHtml>
                {content}
            </ReactMarkdown>
        </div>
    )
})

function MarkdownCodeBlock({ children }: ComponentPropsWithoutRef<'pre'>) {
    const code = Children.toArray(children)[0]
    if (!isValidElement<ComponentPropsWithoutRef<'code'>>(code)) {
        return <pre>{children}</pre>
    }
    const language = codeLanguageLabel(code.props.className)
    const content = extractTextContent(code.props.children).replace(/\n$/, '')

    return (
        <div className="markdown-code-block">
            <div className="markdown-code-toolbar">
                <span className="markdown-code-language">{language}</span>
                <CopyIconButton
                    className="markdown-code-copy-button"
                    copiedLabel={`${language} 代码已复制`}
                    label={`复制 ${language} 代码`}
                    onCopy={() => copyText(content)}
                />
            </div>
            <pre>{code}</pre>
        </div>
    )
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

export const HistoryItemView = memo(function HistoryItemView({ agentProfile, item }: { agentProfile: string; item: AgentHistoryItem }) {
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
    return (
        <article className={`message message-${item.message.role}`}>
            <div className="message-meta">
                <strong>{item.message.role === 'assistant' ? agentProfile : '你'}</strong>
                <time dateTime={item.message.at} title={item.message.at}>{formatMessageTime(item.message.at)}</time>
            </div>
            {item.message.role === 'assistant' ? (
                <MessageMarkdown content={item.message.content} />
            ) : (
                <p>{item.message.content}</p>
            )}
            <MessageCopyButton content={item.message.content} />
        </article>
    )
}, (prev, next) => {
    if (prev.agentProfile !== next.agentProfile) return false
    if (prev.item.kind !== next.item.kind) return false
    if (prev.item.sequence !== next.item.sequence) return false
    if (prev.item.at !== next.item.at) return false
    if (prev.item.message?.content !== next.item.message?.content) return false
    if (prev.item.execution?.id !== next.item.execution?.id) return false
    return true
})

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
