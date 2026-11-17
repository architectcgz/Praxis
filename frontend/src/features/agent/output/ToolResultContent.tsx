import { Fragment } from 'react'
import { isDiffContent, DiffCode, MessageMarkdown } from './MarkdownViews'

const resultFieldLabels: Record<string, string> = {
    agentId: '代理 ID',
    runId: '执行 ID',
    messageId: '消息 ID',
    reportId: '报告 ID',
    status: '状态',
    outcome: '结果',
    outputFile: '输出文件',
    size: '大小',
    message: '说明',
    error: '错误',
    exitCode: '退出码',
}

export function ToolResultContent({ content, toolName }: { content: string; toolName?: string }) {
    if (isDiffContent(content)) return <DiffCode content={content} />
    if (toolName === 'read_file') return <pre className="output-text output-file-content">{content}</pre>
    const structured = parseStructuredJSON(content)
    if (structured !== undefined) return <StructuredResult value={structured} />
    if (content.includes('```') || /(?:^|\n)\s{0,3}(?:#{1,6}\s|>\s|[-*+]\s|\d+\.\s)/.test(content)) return <MessageMarkdown content={content} />
    return <pre className="output-text">{content}</pre>
}

function parseStructuredJSON(content: string): unknown | undefined {
    const trimmed = content.trim()
    if (!(trimmed.startsWith('{') && trimmed.endsWith('}')) && !(trimmed.startsWith('[') && trimmed.endsWith(']'))) {
        return undefined
    }
    try {
        const parsed = JSON.parse(trimmed) as unknown
        return typeof parsed === 'object' && parsed !== null ? parsed : undefined
    } catch {
        return undefined
    }
}

function StructuredResult({ value }: { value: unknown }) {
    if (Array.isArray(value)) {
        if (value.length === 0) return <p className="output-muted">空列表</p>
        if (value.every((item) => typeof item !== 'object' || item === null)) {
            return (
                <span className="output-kv-chips">
                    {value.map((item, index) => <code className="output-kv-chip" key={index}>{String(item)}</code>)}
                </span>
            )
        }
        return (
            <div className="output-kv-items">
                {value.map((item, index) => (
                    <div className="output-kv-item" key={index}>
                        {typeof item === 'object' && item !== null ? <StructuredObject record={item as Record<string, unknown>} /> : <code>{String(item)}</code>}
                    </div>
                ))}
            </div>
        )
    }
    return <StructuredObject record={value as Record<string, unknown>} />
}

function StructuredObject({ record }: { record: Record<string, unknown> }) {
    const entries = Object.entries(record).filter(([, value]) => value !== undefined && value !== '' && value !== null)
    if (entries.length === 0) return <p className="output-muted">空对象</p>
    return (
        <dl className="output-kv-grid">
            {entries.map(([key, value]) => (
                <Fragment key={key}>
                    <dt>{resultFieldLabels[key] || key}</dt>
                    <dd>{renderResultValue(key, value)}</dd>
                </Fragment>
            ))}
        </dl>
    )
}

function renderResultValue(key: string, value: unknown) {
    if (key === 'run' && typeof value === 'object' && value !== null && !Array.isArray(value)) {
        const run = value as Record<string, unknown>
        const parts = [
            typeof run.Status === 'string' ? run.Status : '',
            typeof run.Outcome === 'string' && run.Outcome ? run.Outcome : '',
            typeof run.ID === 'string' ? run.ID : '',
        ].filter(Boolean)
        if (parts.length > 0) return <code>{parts.join(' · ')}</code>
    }
    if (Array.isArray(value)) {
        if (value.length === 0) return <span className="output-muted">空</span>
        if (value.every((item) => typeof item !== 'object' || item === null)) {
            return (
                <span className="output-kv-chips">
                    {value.map((item, index) => <code className="output-kv-chip" key={index}>{String(item)}</code>)}
                </span>
            )
        }
        return <pre className="output-text">{JSON.stringify(value, null, 2)}</pre>
    }
    if (typeof value === 'object' && value !== null) {
        return <pre className="output-text">{JSON.stringify(value, null, 2)}</pre>
    }
    if (key === 'size' && typeof value === 'number') return <span>{value} 字节</span>
    if (typeof value === 'boolean') return <code>{value ? '是' : '否'}</code>
    if (key === 'message' || key === 'error') return <span className="output-kv-text">{String(value)}</span>
    return <code>{String(value)}</code>
}
