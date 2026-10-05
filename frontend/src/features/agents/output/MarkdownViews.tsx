import { Children, createContext, isValidElement, memo, useContext, useEffect, useMemo, useRef, useState, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { Check, ChevronDown, Copy } from 'lucide-react'
import ReactMarkdown, { defaultUrlTransform, type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { openExternalURL } from '../../../api/filePreview'
import { parseMessageLink } from './messageLinks'

/** 由会话预览容器提供打开文件的动作；预览内的相对链接以当前文件为基准。 */
export const FilePreviewContext = createContext<{ openFile: (path: string) => void; basePath?: string } | null>(null)

const markdownPlugins = [remarkGfm]
const markdownComponents: Components = {
    table: MarkdownTable,
    a: MessageLink,
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

export const MessageMarkdown = memo(function MessageMarkdown({ content, streaming = false }: { content: string; streaming?: boolean }) {
    const components = useMemo<Components>(() => ({
        ...markdownComponents,
        pre: (props) => <MarkdownCodeBlock {...props} streaming={streaming} />,
    }), [streaming])

    return (
        <div className="message-markdown">
            <ReactMarkdown components={components} remarkPlugins={markdownPlugins} skipHtml urlTransform={(url, key) => (
                key === 'href' ? parseMessageLink(url).kind === 'blocked' ? '' : url : defaultUrlTransform(url)
            )}>
                {content}
            </ReactMarkdown>
        </div>
    )
})

function MessageLink({ href = '', children, title }: ComponentPropsWithoutRef<'a'>) {
    const preview = useContext(FilePreviewContext)
    const link = parseMessageLink(href, preview?.basePath)
    if (link.kind === 'file') {
        return <button className="message-file-link" type="button" disabled={!preview}
            onClick={() => preview?.openFile(link.path)}>{children}</button>
    }
    if (link.kind === 'external') {
        return <a href={link.url} target="_blank" rel="noopener noreferrer" title={title || '在浏览器中打开'}
            onClick={(event) => { event.preventDefault(); openExternalURL(link.url) }}>{children}</a>
    }
    return <span>{children}</span>
}

function MarkdownCodeBlock({ children, streaming }: ComponentPropsWithoutRef<'pre'> & { streaming: boolean }) {
    const code = Children.toArray(children)[0]
    if (!isValidElement<ComponentPropsWithoutRef<'code'>>(code)) {
        return <pre>{children}</pre>
    }
    const language = codeLanguageLabel(code.props.className)
    const content = extractTextContent(code.props.children).replace(/\n$/, '')
    const isDiff = isDiffContent(content, code.props.className)
    const changes = isDiff ? codeLineChanges(content, code.props.className) : null

    return (
        <div className="markdown-code-block">
            <details open={streaming || undefined}>
                <summary className="markdown-code-toolbar">
                    <span className="markdown-code-language">{language}</span>
                    {changes && <span className="markdown-code-changes"><span>+{changes.added}</span><span>-{changes.removed}</span></span>}
                    <ChevronDown size={14} strokeWidth={1.8} aria-hidden="true" />
                </summary>
                <pre className={isDiff ? 'markdown-code-diff' : undefined}>
                    {isDiff ? <DiffCodeLines content={content} /> : code}
                </pre>
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

export function DiffCode({ content }: { content: string }) {
    return (
        <pre className="diff-code">
            <DiffCodeLines content={content} />
        </pre>
    )
}

function DiffCodeLines({ content }: { content: string }) {
    const lines = content.split('\n')
    return (
        <code className="diff-code-lines">
            {lines.map((line, index) => (
                <span className={`markdown-code-line markdown-code-line-${diffLineKind(line)}`} key={`${index}-${line}`}>
                    {line || ' '}
                </span>
            ))}
        </code>
    )
}

export function isDiffContent(content: string, className?: string) {
    const lines = content ? content.split('\n') : []
    return /language-(diff|patch)\b/i.test(className || '') || lines.some((line) => (
        line.startsWith('diff --git ') ||
        line.startsWith('@@ ') ||
        line.startsWith('*** Begin Patch') ||
        line.startsWith('*** Add File') ||
        line.startsWith('*** Update File') ||
        line.startsWith('*** Delete File') ||
        line.startsWith('+++ ') ||
        line.startsWith('--- ')
    ))
}

function codeLineChanges(content: string, className?: string) {
    const lines = content ? content.split('\n') : []
    const isDiff = isDiffContent(content, className)
    if (!isDiff) return { added: lines.length, removed: 0 }
    return {
        added: lines.filter((line) => line.startsWith('+') && !line.startsWith('+++')).length,
        removed: lines.filter((line) => line.startsWith('-') && !line.startsWith('---')).length,
    }
}

function diffLineKind(line: string) {
    if (line.startsWith('+') && !line.startsWith('+++')) return 'added'
    if (line.startsWith('-') && !line.startsWith('---')) return 'removed'
    return 'context'
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
    if (!language) return '纯文本'
    return codeLanguageLabels[language] || language.toUpperCase()
}

export type CopyIconButtonProps = {
    className: string
    copiedLabel: string
    label: string
    onCopy: () => Promise<void>
}

export function CopyIconButton({ className, copiedLabel, label, onCopy }: CopyIconButtonProps) {
    const [copied, setCopied] = useState(false)

    useEffect(() => {
        if (!copied) return
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
    if (!navigator.clipboard?.writeText) throw new Error('Clipboard access is unavailable')
    await navigator.clipboard.writeText(content)
}

function tableText(table: HTMLTableElement | null) {
    if (!table) return ''
    return Array.from(table.rows, (row) => (
        Array.from(row.cells, (cell) => cell.textContent?.replace(/\s+/g, ' ').trim() || '').join('\t')
    )).join('\n')
}

export function extractTextContent(children: ReactNode): string {
    if (typeof children === 'string') return children
    if (Array.isArray(children)) return children.map(extractTextContent).join('')
    if (isValidElement<{ children?: ReactNode }>(children)) return extractTextContent(children.props.children)
    return ''
}
