import { Brain, ChevronDown } from 'lucide-react'
import { MessageMarkdown } from './MarkdownViews'

/** 以原生折叠区展示思考；历史默认收起，流式默认展开且允许用户手动折叠。 */
export function ThinkingDetails({ content, streaming = false }: { content: string; streaming?: boolean }) {
    // 相邻的加粗片段缺少分隔时，Markdown 会把四个星号显示为正文。
    const formattedContent = content.replace(/(\S)\*{4}(?=\S)/g, '$1**\n\n**')
    return (
        <details className={`thinking-details${streaming ? ' thinking-details-streaming' : ''}`} open={streaming || undefined} aria-label="思考内容">
            <summary className="thinking-toggle" title="展开或收起思考内容">
                <Brain className="thinking-mark" size={14} strokeWidth={1.8} aria-hidden="true" />
                <span className="thinking-label">{streaming ? '思考中' : '思考过程'}</span>
                <span className="thinking-preview" aria-hidden="true">{thinkingPreview(formattedContent)}</span>
                <ChevronDown className="thinking-chevron" size={14} strokeWidth={1.8} aria-hidden="true" />
            </summary>
            <div className="thinking-content">
                <MessageMarkdown content={formattedContent} streaming={streaming} />
            </div>
        </details>
    )
}

function thinkingPreview(content: string) {
    const preview = content.replace(/[`*_~]/g, '').replace(/\s+/g, ' ').trim()
    if (preview.length <= 140) return preview || '思考内容'
    return `${preview.slice(0, 140)}…`
}
