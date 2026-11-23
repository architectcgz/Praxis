import { Children, isValidElement, memo, useDeferredValue, useMemo, useRef, useState, type ComponentPropsWithoutRef, type ReactNode } from 'react'
import { AlignLeft, ChevronRight, Code2 } from 'lucide-react'
import ReactMarkdown, { defaultUrlTransform, type Components } from 'react-markdown'
import remarkGfm from 'remark-gfm'
import { openExternalURL } from '../../../api/externalLinks'
import { readableError } from '../../../shared/errors'
import { parseMessageLink } from './messageLinks'
import { CopyIconButton } from './CopyIconButton'

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
    const deferredContent = useDeferredValue(content)
    // 流式事件同步到达时复用已渲染内容，Markdown 解析不能阻塞计时和输入；落盘内容立即展示。
    return <MarkdownContent content={streaming ? deferredContent : content} streaming={streaming} />
})

const MarkdownContent = memo(function MarkdownContent({ content, streaming }: { content: string; streaming: boolean }) {
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
    const [error, setError] = useState('')
    const link = parseMessageLink(href)
    const openExternal = async (url: string) => {
        setError('')
        try {
            await openExternalURL(url)
        } catch (reason) {
            setError(`打开链接失败：${readableError(reason)}`)
        }
    }
    if (link.kind === 'file') return <code title={link.path}>{children}</code>
    if (link.kind === 'external') {
        return <><a href={link.url} target="_blank" rel="noopener noreferrer" title={title || '在浏览器中打开'}
            onClick={(event) => { event.preventDefault(); void openExternal(link.url) }}>{children}</a>
            {error && <span role="alert">（{error}，可再次点击链接重试）</span>}
        </>
    }
    return <span>{children}</span>
}

function MarkdownCodeBlock({ children }: ComponentPropsWithoutRef<'pre'> & { streaming: boolean }) {
    const code = Children.toArray(children)[0]
    if (!isValidElement<ComponentPropsWithoutRef<'code'>>(code)) {
        return <pre className="markdown-plain-pre">{children}</pre>
    }
    const { label: language, isPlain } = parseCodeLanguage(code.props.className)
    const content = extractTextContent(code.props.children).replace(/\n$/, '')
    const isDiff = isDiffContent(content, code.props.className)
    const plainBlock = isPlain && !isDiff
    const changes = isDiff ? codeLineChanges(content, code.props.className) : null
    const lineCount = content ? content.split('\n').length : 0

    return (
        <div className={`markdown-code-block ${plainBlock ? 'is-plain' : 'instrument'}`}>
            <details open>
                <summary className="markdown-code-toolbar">
                    <ChevronRight className="markdown-code-chevron" size={13} strokeWidth={2} aria-hidden="true" />
                    <span className="markdown-code-kind">
                        {plainBlock
                            ? <AlignLeft size={12} strokeWidth={1.85} aria-hidden="true" />
                            : <Code2 size={12} strokeWidth={1.85} aria-hidden="true" />}
                        <span className="markdown-code-language">{language}</span>
                    </span>
                    {changes ? (
                        <span className="markdown-code-changes">
                            <span>+{changes.added}</span>
                            <span>-{changes.removed}</span>
                        </span>
                    ) : lineCount > 0 ? (
                        <span className="markdown-code-lines">{lineCount} 行</span>
                    ) : null}
                </summary>
                <pre className={isDiff ? 'markdown-code-diff' : undefined}>
                    {isDiff ? <DiffCodeLines content={content} /> : code}
                </pre>
            </details>
            <CopyIconButton
                className="markdown-code-copy-button"
                copiedLabel={plainBlock ? '纯文本已复制' : `${language} 代码已复制`}
                label={plainBlock ? '复制纯文本' : `复制 ${language} 代码`}
                getText={() => content}
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
                    getText={() => tableText(tableRef.current)}
                />
            </div>
            <div className="markdown-table-scroll">
                <table ref={tableRef}>{children}</table>
            </div>
        </div>
    )
}

function parseCodeLanguage(className: string | undefined): { label: string; isPlain: boolean } {
    const language = className?.match(/language-([a-z0-9+-]+)/i)?.[1]?.toLowerCase()
    if (!language || language === 'text' || language === 'txt' || language === 'plaintext' || language === 'plain') {
        return { label: '纯文本', isPlain: true }
    }
    return { label: codeLanguageLabels[language] || language.toUpperCase(), isPlain: false }
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
